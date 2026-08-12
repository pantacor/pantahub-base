// Copyright 2016-2026  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

// Package redirecturi implements allowlist validation of OAuth redirect_uri
// values against the callback URLs registered on an application.
//
// A candidate matches when its scheme, host and port equal those of a
// registered entry and its path sits at or beneath the registered path. An
// entry registered as a bare origin therefore covers every path on that origin,
// which is how existing registrations are written.
//
// The host and port are compared as parsed components, never as string
// prefixes: https://app.example.com does not cover
// https://app.example.com.evil.example. Paths are cleaned before comparison so
// a registered /callback cannot be escaped with /callback/../elsewhere.
//
// One exemption applies to the loopback interface: RFC 8252 section 7.3 has
// native clients bind an ephemeral port, so a target on localhost / 127.0.0.1 /
// ::1 matches a registered loopback entry regardless of port.
package redirecturi

import (
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"path"
	"strings"
)

// Validation failures. These are deliberately coarse: the caller returns them
// to unauthenticated clients, so they must not disclose what was registered.
var (
	// ErrNotRegistered is returned when the candidate does not match any
	// registered callback URL for the application.
	ErrNotRegistered = errors.New("redirect_uri does not match a registered callback URL for this application")

	// ErrMalformed is returned when the candidate cannot be parsed or carries
	// components that are never legitimate in a redirect target.
	ErrMalformed = errors.New("redirect_uri is malformed")

	// ErrNoneRegistered is returned when the application has no callback URLs
	// registered at all. This is a hard failure rather than a skipped check.
	ErrNoneRegistered = errors.New("application has no registered callback URLs")

	// ErrNotAllowedOrigin is returned when a return target does not sit on one
	// of the origins this deployment serves its web interface from.
	ErrNotAllowedOrigin = errors.New("redirect_uri is not on an allowed origin")
)

// dangerousSchemes can execute script or inline a payload and are rejected
// even if somebody manages to register one.
var dangerousSchemes = map[string]bool{
	"javascript": true,
	"data":       true,
	"vbscript":   true,
	"file":       true,
	"blob":       true,
}

// loopbackHosts are treated as the same interface. A native client registers
// one of these and then binds whatever ephemeral port it can get.
var loopbackHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// parsed is a redirect URI reduced to the components that participate in
// matching, with default ports and empty paths normalised away.
type parsed struct {
	scheme   string
	host     string
	port     string
	path     string
	loopback bool
}

// parse normalises rawURI for comparison, rejecting anything that is never a
// legitimate redirect target.
func parse(rawURI string) (*parsed, error) {
	if rawURI == "" {
		return nil, ErrMalformed
	}

	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, ErrMalformed
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme == "" || dangerousSchemes[scheme] {
		return nil, ErrMalformed
	}

	// Credentials in the authority are a classic way to make a hostile host
	// look like a trusted one to a human reading the URL.
	if u.User != nil {
		return nil, ErrMalformed
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return nil, ErrMalformed
	}

	// A fragment on the candidate would collide with the fragment we append
	// when handing back an implicit token.
	if u.Fragment != "" || strings.Contains(rawURI, "#") {
		return nil, ErrMalformed
	}

	return &parsed{
		scheme:   scheme,
		host:     host,
		port:     normalisePort(scheme, u.Port()),
		path:     cleanPath(u.Path),
		loopback: loopbackHosts[host],
	}, nil
}

// ValidateURI reports whether rawURI is well-formed and safe to register as a
// redirect URI: non-empty, parseable, a non-dangerous scheme, no embedded
// credentials, a non-empty host, and no fragment. It does not require HTTPS so
// that loopback development URIs remain usable.
func ValidateURI(rawURI string) error {
	_, err := parse(rawURI)
	return err
}

// cleanPath normalises a URL path for comparison, resolving any . and ..
// segments so a registered prefix cannot be escaped by traversing out of it.
func cleanPath(raw string) string {
	if raw == "" {
		return "/"
	}
	return path.Clean(raw)
}

// pathWithin reports whether candidate is the registered path or sits beneath
// it. A registered path of "/" covers the whole origin.
func pathWithin(registered, candidate string) bool {
	if registered == "/" {
		return true
	}
	if registered == candidate {
		return true
	}
	return strings.HasPrefix(candidate, registered+"/")
}

// normalisePort collapses the scheme's default port to the empty string so
// https://example.com and https://example.com:443 compare equal.
func normalisePort(scheme, port string) string {
	switch {
	case port == "":
		return ""
	case scheme == "http" && port == "80":
		return ""
	case scheme == "https" && port == "443":
		return ""
	}
	return port
}

// equal reports whether a candidate matches a registered URI: same origin, and
// a path at or beneath the registered one.
func equal(registered, candidate *parsed) bool {
	if registered.scheme != candidate.scheme {
		return false
	}

	// RFC 8252 section 7.3: the loopback port is assigned at runtime, so it is
	// excluded from the comparison. The host is excluded too because the
	// registered spelling ("localhost") and the bound literal ("127.0.0.1")
	// address the same interface on the user's own machine.
	if registered.loopback && candidate.loopback {
		return pathWithin(registered.path, candidate.path)
	}

	return registered.host == candidate.host &&
		registered.port == candidate.port &&
		pathWithin(registered.path, candidate.path)
}

// Match reports whether candidate matches one of the registered callback URLs:
// same origin, with a path at or beneath the registered one. Registered entries
// that fail to parse are skipped rather than treated as wildcards.
func Match(registered []string, candidate string) bool {
	c, err := parse(candidate)
	if err != nil {
		return false
	}

	for _, entry := range registered {
		r, err := parse(entry)
		if err != nil {
			continue
		}
		if equal(r, c) {
			return true
		}
	}

	return false
}

// MatchOrigin reports whether candidate sits on one of the allowed origins,
// comparing scheme, host and port and ignoring the path.
//
// This is used for return targets that are our own web interface, where the
// whole origin is trusted and the UI owns which path it lands on. It must not
// be used for third-party callbacks: those go through Match.
func MatchOrigin(allowedOrigins []string, candidate string) bool {
	c, err := parse(candidate)
	if err != nil {
		return false
	}

	for _, entry := range allowedOrigins {
		o, err := parse(entry)
		if err != nil {
			continue
		}
		if o.scheme == c.scheme && o.host == c.host && o.port == c.port {
			return true
		}
	}

	return false
}

// ValidateOrigin checks candidate against the allowed origins and emits an
// audit event when it is rejected.
func ValidateOrigin(allowedOrigins []string, candidate string, ctx AuditContext) error {
	if candidate == "" {
		return ErrMalformed
	}

	if len(allowedOrigins) == 0 {
		ctx.reject(candidate, ErrNoneRegistered)
		return ErrNoneRegistered
	}

	if !MatchOrigin(allowedOrigins, candidate) {
		if _, err := parse(candidate); err != nil {
			ctx.reject(candidate, ErrMalformed)
			return ErrMalformed
		}
		ctx.reject(candidate, ErrNotAllowedOrigin)
		return ErrNotAllowedOrigin
	}

	return nil
}

// Validate checks candidate against the registered callback URLs and emits an
// audit event when it is rejected. ctx carries the request metadata used for
// monitoring; it is not consulted for the decision.
//
// An application that has registered no callbacks at all is left unconstrained.
// Registrations predate the requirement to declare a callback, so rejecting
// them would break clients that work today. Each occurrence is recorded under
// AuditEventUnvalidated so the outstanding registrations can be found and
// completed, after which those applications become constrained automatically.
func Validate(registered []string, candidate string, ctx AuditContext) error {
	if candidate == "" {
		return ErrMalformed
	}

	if len(registered) == 0 {
		ctx.unvalidated(candidate)
		return nil
	}

	if !Match(registered, candidate) {
		if _, err := parse(candidate); err != nil {
			ctx.reject(candidate, ErrMalformed)
			return ErrMalformed
		}
		ctx.reject(candidate, ErrNotRegistered)
		return ErrNotRegistered
	}

	return nil
}

// AuditContext carries the request metadata attached to a rejection event so
// that unexpected redirect targets can be alerted on.
type AuditContext struct {
	// Flow names the OAuth flow that produced the request, e.g. "social_login".
	Flow string

	// ClientID is the application the redirect was validated against.
	ClientID string

	// Service is the upstream identity provider, where one applies.
	Service string

	// RemoteAddr is the client address as seen by the API.
	RemoteAddr string

	// UserAgent is the requesting user agent, truncated by the emitter.
	UserAgent string
}

// auditEvent is the structured record written for every rejection. The event
// name is stable so log pipelines can key alerts off it.
type auditEvent struct {
	Event       string `json:"event"`
	Severity    string `json:"severity"`
	Flow        string `json:"flow,omitempty"`
	ClientID    string `json:"client_id,omitempty"`
	Service     string `json:"service,omitempty"`
	RedirectURI string `json:"redirect_uri"`
	Reason      string `json:"reason"`
	RemoteAddr  string `json:"remote_addr,omitempty"`
	UserAgent   string `json:"user_agent,omitempty"`
}

// AuditEventName is the stable identifier emitted on every rejected redirect
// target. Alert on a rising rate of these: a spike means somebody is probing
// the allowlist, and any occurrence with an external host is worth a look.
const AuditEventName = "oauth_redirect_uri_rejected"

// AuditEventUnvalidated is the stable identifier emitted when a redirect target
// is allowed only because the application has no registered callbacks. Every
// one of these is an application whose redirect targets are unconstrained;
// treat the list as a backlog to fix rather than as steady state.
const AuditEventUnvalidated = "oauth_redirect_uri_unvalidated"

// LogRejection records a redirect target refused for a reason the matcher does
// not itself evaluate, such as a client_id that resolves to nothing.
func LogRejection(candidate string, reason error, ctx AuditContext) {
	ctx.reject(candidate, reason)
}

// unvalidated records a redirect target that was allowed through only because
// the application declares no callbacks to check it against.
func (c AuditContext) unvalidated(candidate string) {
	event := auditEvent{
		Event:       AuditEventUnvalidated,
		Severity:    "warning",
		Flow:        c.Flow,
		ClientID:    c.ClientID,
		Service:     c.Service,
		RedirectURI: truncate(candidate, 512),
		Reason:      "application has no registered callback URLs, so its redirect targets are unconstrained",
		RemoteAddr:  c.RemoteAddr,
		UserAgent:   truncate(c.UserAgent, 256),
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		log.Printf("%s: redirect_uri=%q", AuditEventUnvalidated, event.RedirectURI)
		return
	}

	log.Println(string(encoded))
}

// reject writes the audit record. Rejections are rare by construction, so this
// logs unconditionally rather than sampling.
func (c AuditContext) reject(candidate string, reason error) {
	event := auditEvent{
		Event:       AuditEventName,
		Severity:    "warning",
		Flow:        c.Flow,
		ClientID:    c.ClientID,
		Service:     c.Service,
		RedirectURI: truncate(candidate, 512),
		Reason:      reason.Error(),
		RemoteAddr:  c.RemoteAddr,
		UserAgent:   truncate(c.UserAgent, 256),
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		log.Printf("%s: redirect_uri=%q reason=%q", AuditEventName, event.RedirectURI, event.Reason)
		return
	}

	log.Println(string(encoded))
}

// truncate bounds attacker-controlled strings before they reach the log.
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
