// Copyright (c) 2017-2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package webhooks mounts the customer-facing webhook delivery service as
// a reverse-proxy under /webhooks/ on the api.pantacor.com host.
//
// The actual delivery service (pantahub-webhooks) runs as a separate
// deployment. This module:
//
//  1. Validates the caller's JWT with the same middleware every other
//     module uses.
//
//  2. Enforces the Webhooks scope set.
//
//  3. Forwards the request to the upstream (PANTAHUB_WEBHOOKS_BACKEND)
//     while rewriting headers so the upstream can trust:
//
//     X-Pantahub-Caller   PRN of the request initiator
//     X-Pantahub-Owner    PRN of the resource owner (matches authInfo.Owner)
//     X-Pantahub-Type     caller type (USER / DEVICE / ...)
//     X-Pantahub-Scopes   space-separated scope strings
//
//  4. Strips the inbound Authorization header so the upstream never sees
//     a JWT — it only trusts the headers we set above. The upstream is
//     deployed cluster-internal and not exposed publicly, which is what
//     makes this trust safe.
package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

// App is the webhooks proxy module.
type App struct {
	jwtConfig    *jwtauth.Config
	backend      *url.URL
	proxy        *httputil.ReverseProxy
	proxySecrets [][]byte
}

// New constructs the webhooks proxy app. The backend URL is read from
// PANTAHUB_WEBHOOKS_BACKEND (default http://localhost:12380). Requests
// are forwarded with their full /webhooks/... path, which is the path the
// upstream's router expects.
func New(jwtConfig *jwtauth.Config) *App {
	app := new(App)
	app.jwtConfig = jwtConfig

	backendStr := os.Getenv("PANTAHUB_WEBHOOKS_BACKEND")
	if backendStr == "" {
		backendStr = "http://localhost:12380"
	}
	u, err := url.Parse(backendStr)
	if err != nil {
		//#nosec G706 -- an operator-set env var, already quoted with %q
		log.Fatalf("webhooks: invalid PANTAHUB_WEBHOOKS_BACKEND %q: %v", backendStr, err)
	}
	app.backend = u

	for _, k := range []string{"PANTAHUB_WEBHOOKS_PROXY_SECRET", "PANTAHUB_WEBHOOKS_PROXY_SECRET_V2"} {
		if v := os.Getenv(k); v != "" {
			app.proxySecrets = append(app.proxySecrets, []byte(v))
		}
	}
	if len(app.proxySecrets) == 0 && os.Getenv("PANTAHUB_WEBHOOKS_PROXY_TRUST_INSECURE") != "true" {
		log.Fatalf("webhooks: PANTAHUB_WEBHOOKS_PROXY_SECRET must be set " +
			"(or PANTAHUB_WEBHOOKS_PROXY_TRUST_INSECURE=true for local dev)")
	}

	app.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			// req.URL.Path is already /webhooks/<rest>, the layout the
			// upstream also serves via the optional hooks.pantacor.com ingress.
			req.URL.RawPath = ""
			req.Host = u.Host
		},
		ModifyResponse: func(resp *http.Response) error {
			// Hide the upstream identity in case it leaks via Server header.
			resp.Header.Del("Server")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("webhooks proxy: upstream error: %v", err)
			http.Error(w, "webhooks backend unavailable", http.StatusBadGateway)
		},
	}

	return app
}

// Mount registers webhooks on echo with its previous middleware stack.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/webhooks"

	// /webhooks/event-types is public (no auth); every other route requires a JWT.
	needsAuth := func(r *http.Request) bool {
		return !strings.HasPrefix(r.URL.Path, "/event-types")
	}

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/webhooks:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "webhooks"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{
				"Accept", "Content-Type", "Content-Length",
				"Origin", "Authorization",
				"X-Trace-ID", "Trace-Id", "x-request-id", "X-Request-ID",
				"TraceID", "ParentID",
				"Uber-Trace-ID", "uber-trace-id", "traceparent", "tracestate",
			},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		echoutil.If(prefix, needsAuth, echoutil.JWT(app.jwtConfig)),
		echoutil.If(prefix, needsAuth, echoutil.Auth()),
	)

	read := echoutil.ScopeFilterMW([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.Webhooks,
		utils.Scopes.ReadWebhooks,
	})
	write := echoutil.ScopeFilterMW([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Webhooks,
		utils.Scopes.WriteWebhooks,
	})

	// Public catalog.
	g.GET("/event-types", app.handleProxy)

	// Events.
	g.GET("/events", app.handleProxy, read)
	g.GET("/events/:eid", app.handleProxy, read)
	g.GET("/events/:eid/deliveries", app.handleProxy, read)
	g.POST("/events/:eid/redeliver", app.handleProxy, write)

	// Subscriptions.
	g.GET("/", app.handleProxy, read)
	g.POST("/", app.handleProxy, write)
	g.GET("/:id", app.handleProxy, read)
	g.PUT("/:id", app.handleProxy, write)
	g.PATCH("/:id", app.handleProxy, write)
	g.DELETE("/:id", app.handleProxy, write)
	g.POST("/:id/rotate-secret", app.handleProxy, write)
	g.POST("/:id/test", app.handleProxy, write)

	// Deliveries.
	g.GET("/:id/deliveries", app.handleProxy, read)
	g.GET("/:id/deliveries/:did", app.handleProxy, read)
	g.POST("/:id/deliveries/:did/replay", app.handleProxy, write)
}

// handleProxy strips the inbound Authorization header, attaches the
// resolved identity as X-Pantahub-* headers, then forwards via the
// reverse proxy. The Director set in New() takes care of the URL.
func (app *App) handleProxy(c *echo.Context) error {
	r := c.Request()
	// Strip headers the upstream must never see.
	r.Header.Del("Authorization")
	r.Header.Del("Cookie")
	// Defense in depth: strip any client-supplied trust headers; we set
	// them ourselves below.
	r.Header.Del("X-Pantahub-Caller")
	r.Header.Del("X-Pantahub-Owner")
	r.Header.Del("X-Pantahub-Type")
	r.Header.Del("X-Pantahub-Scopes")
	r.Header.Del("X-Pantahub-Proxy-Timestamp")
	r.Header.Del("X-Pantahub-Proxy-Signature")

	authInfo := echoutil.AuthInfo(c)
	var owner, caller string
	if authInfo != nil {
		caller = string(authInfo.Caller)
		owner = string(authInfo.Owner)
		r.Header.Set("X-Pantahub-Caller", caller)
		r.Header.Set("X-Pantahub-Owner", owner)
		r.Header.Set("X-Pantahub-Type", authInfo.CallerType)
		if len(authInfo.Scopes) > 0 {
			r.Header.Set("X-Pantahub-Scopes", strings.Join(authInfo.Scopes, " "))
		}
	}

	// Sign with the active secret. The hooks service accepts either
	// PANTAHUB_WEBHOOKS_PROXY_SECRET or _V2 to allow rotation.
	//
	// v2 canonical string — MUST stay in lockstep with the verifier in
	// pantahub-webhooks/internal/api/proxytrust.go. It binds the query, a
	// body digest, a nonce, and the trusted identity/authorization headers,
	// so an on-path actor cannot keep a captured MAC while swapping a PUT
	// body, GET filters, or the Type/Scopes headers, and cannot replay the
	// request (the verifier remembers nonces for the skew window).
	//
	// Deploy ordering: the upstream verifier understands v2 before this
	// signer emits it, so roll out pantahub-webhooks first. The upstream's
	// PH_WEBHOOKS_PROXY_ALLOW_LEGACY_SIGNATURE flag covers old signers
	// during the transition; this side no longer emits v1.
	if len(app.proxySecrets) > 0 {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		nonce, err := newNonce()
		if err != nil {
			log.Printf("webhooks proxy: nonce: %v", err)
			return echoutil.Error(c, "internal error", http.StatusInternalServerError)
		}

		// The body digest is part of the signed string, so the body has to
		// be buffered and handed back for the proxy to forward. These are
		// small JSON management-API payloads, never streams.
		var body []byte
		if r.Body != nil {
			body, err = io.ReadAll(r.Body)
			if err != nil {
				return echoutil.Error(c, "invalid body: "+err.Error(), http.StatusBadRequest)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		bodySum := sha256.Sum256(body)

		// Sign the full /webhooks/... path the upstream's router sees.
		signedPath := r.URL.Path

		// Sign the header values as actually set above, so signer and
		// verifier read identical strings even when authInfo was nil.
		base := strings.Join([]string{
			"v2", ts, nonce, r.Method, signedPath,
			canonicalQuery(r.URL.RawQuery),
			hex.EncodeToString(bodySum[:]),
			r.Header.Get("X-Pantahub-Owner"),
			r.Header.Get("X-Pantahub-Caller"),
			r.Header.Get("X-Pantahub-Type"),
			r.Header.Get("X-Pantahub-Scopes"),
		}, "\n")
		mac := hmac.New(sha256.New, app.proxySecrets[0])
		mac.Write([]byte(base))
		r.Header.Set("X-Pantahub-Proxy-Timestamp", ts)
		r.Header.Set("X-Pantahub-Proxy-Nonce", nonce)
		r.Header.Set("X-Pantahub-Proxy-Signature", "v2="+hex.EncodeToString(mac.Sum(nil)))
	}

	app.proxy.ServeHTTP(flushSafeWriter{c.Response()}, r)
	return nil
}

// newNonce returns a fresh random hex nonce for the proxy signature. The
// verifier rejects a nonce it has already seen within the skew window,
// which is what turns the timestamp check into actual replay protection.
func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// canonicalQuery renders a raw query string in the canonical form both
// sides of the proxy protocol sign: keys sorted, values sorted within a
// key, every key and value re-escaped. MUST stay in lockstep with
// CanonicalQuery in pantahub-webhooks/internal/api/proxytrust.go.
func canonicalQuery(rawQuery string) string {
	q, err := url.ParseQuery(rawQuery)
	if err != nil || len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(v))
		}
	}
	return b.String()
}

// flushSafeWriter gives httputil.ReverseProxy an http.Flusher it can always
// call.
//
// pantahub-base wraps the response writer in
// datacounter.ResponseWriterCounter, which has no Flush method, and echo's
// Response.Flush panics when its writer cannot flush. ReverseProxy flushes
// from the maxLatencyWriter goroutine, so that panic unwinds outside the
// recover middleware and kills the whole process rather than failing one
// request.
//
// Implementing Flush here means the proxy calls this method instead, and the
// flush degrades to a no-op when nothing underneath supports it.
type flushSafeWriter struct {
	http.ResponseWriter
}

func (f flushSafeWriter) Flush() {
	fl, ok := f.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	// Implementing http.Flusher is not proof a writer can actually flush:
	// echo's Response satisfies the interface but panics when its own
	// writer cannot flush, e.g. the datacounter wrapper underneath. Recover here because this runs on
	// ReverseProxy's maxLatencyWriter goroutine, outside the reach of the
	// recover middleware, where a panic kills the process instead of the
	// request. Losing an early flush only delays bytes; the response is
	// still written in full when the handler returns.
	defer func() { _ = recover() }()
	fl.Flush()
}
