// Copyright (c) 2026 Pantacor Ltd.
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

// Package cimd resolves OAuth Client ID Metadata Documents: a client that was
// never registered here identifies itself with an https URL, and the document
// served at that URL says what the client is called and where it may be
// redirected to. It is how MCP clients such as claude.ai introduce themselves
// without every deployment keeping a client record for them.
//
// Resolving one means this server fetches a URL chosen by an unauthenticated
// caller, which is a server-side request forgery primitive unless it is fenced
// in. The fence is: https only, no redirects, a small and slow-loris-proof
// response budget, and a dialer that refuses every address that is not public
// at the moment of connecting, so a hostname cannot resolve to something
// internal, and cannot be re-pointed between a check and the connection.
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/safehttp"
)

const (
	// EnvEnabled turns URL client ids on. Off, a URL is just an unknown
	// client, exactly as before this package existed.
	EnvEnabled = "PANTAHUB_OAUTH_CIMD_ENABLED"

	// EnvAllowedHosts optionally restricts which hosts a client id may live
	// on, as a comma separated list such as "claude.ai". Empty allows any
	// public host. A deployment that only wants to admit the clients it knows
	// should set it: consent shows the user the host, but a list is firmer.
	EnvAllowedHosts = "PANTAHUB_OAUTH_CIMD_ALLOWED_HOSTS"

	fetchTimeout = 5 * time.Second
	maxDocument  = 64 << 10
	maxURLLength = 2048

	minCacheTTL      = 5 * time.Minute
	maxCacheTTL      = 24 * time.Hour
	defaultCacheTTL  = time.Hour
	negativeCacheTTL = time.Minute
	maxCacheEntries  = 512
)

var (
	// ErrNotClientIDURL means the client id is not a metadata document URL at
	// all, so the caller should look it up some other way.
	ErrNotClientIDURL = errors.New("client_id is not a metadata document url")

	// ErrHostNotAllowed means the host is not on the configured allow list.
	ErrHostNotAllowed = errors.New("client_id host is not allowed on this server")

	// ErrThrottled means too many documents were fetched recently. It is not
	// cached: the client id may be perfectly good.
	ErrThrottled = errors.New("too many client metadata documents fetched, try again later")

	// ErrBlockedAddress means the host resolved to an address that is not
	// publicly routable.
	ErrBlockedAddress = errors.New("client_id resolves to a non-public address")
)

// Document is the part of a client metadata document this server acts on.
type Document struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// Host is the host the document was served from. It is the only thing about a
// client that the client cannot make up, so it is what consent should show.
func (d *Document) Host() string {
	parsed, err := url.Parse(d.ClientID)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// Enabled reports whether URL client ids are accepted.
func Enabled() bool {
	enabled, err := strconv.ParseBool(utils.GetEnvDefault(EnvEnabled, "false"))
	return err == nil && enabled
}

// IsClientIDURL reports whether clientID has the shape of a metadata document
// URL. Registered applications are PRNs and never match, so the two kinds of
// client id cannot be confused for one another.
func IsClientIDURL(clientID string) bool {
	return strings.HasPrefix(clientID, "https://")
}

// validateClientIDURL applies the rules a client id URL has to meet before it
// is worth a network request.
func validateClientIDURL(clientID string) (*url.URL, error) {
	if !IsClientIDURL(clientID) {
		return nil, ErrNotClientIDURL
	}
	if len(clientID) > maxURLLength {
		return nil, errors.New("client_id url is too long")
	}

	parsed, err := url.Parse(clientID)
	if err != nil {
		return nil, errors.New("client_id url is malformed")
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, errors.New("client_id url must be https")
	}
	if parsed.User != nil {
		return nil, errors.New("client_id url must not carry credentials")
	}
	if parsed.Fragment != "" || strings.Contains(clientID, "#") {
		return nil, errors.New("client_id url must not carry a fragment")
	}
	// A bare origin is not a client id: the path is what tells two clients of
	// one host apart.
	if parsed.Path == "" || parsed.Path == "/" {
		return nil, errors.New("client_id url must have a path")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, errors.New("client_id url must not contain dot segments")
		}
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return nil, errors.New("client_id url must use the default https port")
	}
	if !hostAllowed(parsed.Hostname()) {
		return nil, ErrHostNotAllowed
	}
	// An IP literal names no organisation a user could recognise at consent,
	// and is the usual way to aim a fetch at something internal.
	if net.ParseIP(parsed.Hostname()) != nil {
		return nil, ErrBlockedAddress
	}

	return parsed, nil
}

func hostAllowed(host string) bool {
	allowed := AllowedHosts()
	if len(allowed) == 0 {
		return true
	}
	host = strings.ToLower(host)
	for _, entry := range allowed {
		if entry == host {
			return true
		}
	}
	return false
}

// AllowedHosts is the configured allow list, lowercased.
func AllowedHosts() []string {
	hosts := []string{}
	for _, entry := range strings.Split(utils.GetEnvDefault(EnvAllowedHosts, ""), ",") {
		if entry = strings.ToLower(strings.TrimSpace(entry)); entry != "" {
			hosts = append(hosts, entry)
		}
	}
	return hosts
}

// publicAddress and guardedDial are the guard of utils/safehttp; the
// resolver answers its own ErrBlockedAddress.
func publicAddress(ip net.IP) bool {
	return safehttp.PublicAddress(ip)
}

func guardedDial(network, address string, c syscall.RawConn) error {
	if err := safehttp.DialControl(network, address, c); err != nil {
		if errors.Is(err, safehttp.ErrBlockedAddress) {
			return ErrBlockedAddress
		}
		return err
	}
	return nil
}

func newHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: fetchTimeout, Control: guardedDial}
	return &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			// No proxy: a proxy would make the connection for us and the
			// address check above would only ever see the proxy.
			Proxy:                  nil,
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    fetchTimeout,
			ResponseHeaderTimeout:  fetchTimeout,
			MaxResponseHeaderBytes: 16 << 10,
			DisableKeepAlives:      true,
		},
		// The document has to live at the client id itself. Following a
		// redirect would let one URL speak for another.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Resolver fetches, validates and caches client metadata documents.
type Resolver struct {
	client *http.Client

	// Resolving is reachable without signing in (the authorize endpoint
	// validates a URL client's redirect), so fetches are throttled per host,
	// to keep this server from being used against one, and in total.
	perHost *utils.IPRateLimiter
	total   *utils.IPRateLimiter

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	document *Document
	err      error
	expires  time.Time
}

// NewResolver returns a resolver with the guarded HTTP client.
func NewResolver() *Resolver {
	return NewResolverWithClient(newHTTPClient())
}

// NewResolverWithClient returns a resolver that fetches with client. The
// network guard lives in the client NewResolver builds, so anything else passed
// here gives it up: this exists for tests, which have to serve documents from
// the loopback addresses the guard refuses.
func NewResolverWithClient(client *http.Client) *Resolver {
	return &Resolver{
		client:  client,
		perHost: utils.NewIPRateLimiter(0.2, 10),
		total:   utils.NewIPRateLimiter(2, 50),
		cache:   map[string]cacheEntry{},
	}
}

// Resolve returns the metadata document of clientID. Failures are cached
// briefly too, so a bad client id cannot be used to make this server hammer
// somebody else's.
func (r *Resolver) Resolve(ctx context.Context, clientID string) (*Document, error) {
	parsed, err := validateClientIDURL(clientID)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	entry, cached := r.cache[clientID]
	r.mu.Unlock()
	if cached && time.Now().Before(entry.expires) {
		return entry.document, entry.err
	}

	if !r.perHost.Allow(strings.ToLower(parsed.Hostname())) || !r.total.Allow("") {
		return nil, ErrThrottled
	}

	document, ttl, err := r.fetch(ctx, clientID)
	if err != nil {
		ttl = negativeCacheTTL
	}

	r.mu.Lock()
	if len(r.cache) >= maxCacheEntries {
		r.evictLocked()
	}
	r.cache[clientID] = cacheEntry{document: document, err: err, expires: time.Now().Add(ttl)}
	r.mu.Unlock()

	return document, err
}

// evictLocked drops what has expired, and everything when that is not enough:
// the cache is an optimisation, and an attacker minting client ids must not be
// able to grow it without bound.
func (r *Resolver) evictLocked() {
	now := time.Now()
	for key, entry := range r.cache {
		if now.After(entry.expires) {
			delete(r.cache, key)
		}
	}
	if len(r.cache) >= maxCacheEntries {
		r.cache = map[string]cacheEntry{}
	}
}

func (r *Resolver) fetch(ctx context.Context, clientID string) (*Document, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, 0, errors.New("client_id url is malformed")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pantahub-oauth-client-metadata/1")

	resp, err := r.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return nil, 0, ErrBlockedAddress
		}
		return nil, 0, errors.New("client metadata document could not be fetched")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("client metadata document answered with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return nil, 0, errors.New("client metadata document could not be read")
	}
	if len(body) > maxDocument {
		return nil, 0, errors.New("client metadata document is too large")
	}

	document := &Document{}
	if err := json.Unmarshal(body, document); err != nil {
		return nil, 0, errors.New("client metadata document is not valid json")
	}
	if err := validateDocument(clientID, document); err != nil {
		return nil, 0, err
	}

	return document, cacheTTL(resp.Header.Get("Cache-Control")), nil
}

// validateDocument checks that the document speaks for the URL it was fetched
// from and describes a client this server can serve.
func validateDocument(clientID string, document *Document) error {
	// This equality is the whole trust model: only whoever controls the URL
	// can publish a document that names it.
	if document.ClientID != clientID {
		return errors.New("client metadata document names a different client_id")
	}

	// A client identified by a URL has nowhere to keep a secret.
	switch document.TokenEndpointAuthMethod {
	case "", "none":
	default:
		return errors.New("client metadata document asks for a token endpoint auth method this server does not offer to url clients")
	}

	if len(document.RedirectURIs) == 0 {
		return errors.New("client metadata document lists no redirect_uris")
	}
	for _, redirectURI := range document.RedirectURIs {
		if err := redirecturi.ValidateURI(redirectURI); err != nil {
			return errors.New("client metadata document lists an unusable redirect_uri")
		}
		parsed, err := url.Parse(redirectURI)
		if err != nil {
			return errors.New("client metadata document lists an unusable redirect_uri")
		}
		// Plain http is only tolerable towards the user's own machine.
		if parsed.Scheme == "http" && !redirecturi.IsLoopbackHost(parsed.Hostname()) {
			return errors.New("client metadata document lists a redirect_uri that is neither https nor loopback")
		}
	}

	document.ClientName = Printable(document.ClientName, 80)
	document.ClientURI = SafeLink(document.ClientURI)
	document.LogoURI = SafeLink(document.LogoURI)
	return nil
}

// Printable keeps a self-asserted label from carrying control characters or
// running on forever on the consent page.
func Printable(value string, max int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if runes := []rune(cleaned); len(runes) > max {
		cleaned = string(runes[:max])
	}
	return cleaned
}

// SafeLink keeps a self-asserted link only when it is a plain https URL.
func SafeLink(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || len(raw) > maxURLLength {
		return ""
	}
	return raw
}

var maxAgePattern = regexp.MustCompile(`(?i)(?:^|[,\s])max-age=(\d+)`)

// cacheTTL honours the document's Cache-Control within bounds: long enough
// that a consent flow does not fetch twice, short enough that a client which
// changes its redirect URIs is believed within a day.
func cacheTTL(cacheControl string) time.Duration {
	if strings.Contains(strings.ToLower(cacheControl), "no-store") {
		return minCacheTTL
	}
	match := maxAgePattern.FindStringSubmatch(cacheControl)
	if match == nil {
		return defaultCacheTTL
	}
	seconds, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return defaultCacheTTL
	}
	ttl := time.Duration(seconds) * time.Second
	if ttl < minCacheTTL {
		return minCacheTTL
	}
	if ttl > maxCacheTTL {
		return maxCacheTTL
	}
	return ttl
}
