// Copyright 2026 Pantacor Ltd.
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

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
)

// App is the rest.Api wrapper for the webhooks proxy module.
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	backend       *url.URL
	proxy         *httputil.ReverseProxy
	proxySecrets  [][]byte
}

// New constructs the webhooks proxy app. The backend URL is read from
// PANTAHUB_WEBHOOKS_BACKEND (default http://localhost:12380). Requests
// are forwarded with /webhooks already stripped by the parent mux, so
// the upstream sees the path the underlying service expects.
func New(jwtMiddleware *jwt.JWTMiddleware) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware

	backendStr := os.Getenv("PANTAHUB_WEBHOOKS_BACKEND")
	if backendStr == "" {
		backendStr = "http://localhost:12380"
	}
	u, err := url.Parse(backendStr)
	if err != nil {
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
			// The original request arrives at /webhooks/<rest>. The parent
			// mux has already stripped /webhooks, so req.URL.Path is /<rest>;
			// the upstream's own router expects /webhooks/<rest>, so we
			// re-prefix here. This mirrors the path layout used when the
			// upstream is hit directly via the optional hooks.pantacor.com
			// ingress.
			req.URL.Path = "/webhooks" + req.URL.Path
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

	app.API = rest.NewApi()
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/webhooks:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "webhooks"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})
	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "Content-Length",
			"Origin", "Authorization",
			"X-Trace-ID", "Trace-Id", "x-request-id", "X-Request-ID",
			"TraceID", "ParentID",
			"Uber-Trace-ID", "uber-trace-id", "traceparent", "tracestate",
		},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	// /webhooks/event-types is public (no auth) — every other route requires a JWT.
	app.API.Use(&rest.IfMiddleware{
		Condition: func(r *rest.Request) bool {
			return !strings.HasPrefix(r.URL.Path, "/event-types")
		},
		IfTrue: app.jwtMiddleware,
	})
	app.API.Use(&rest.IfMiddleware{
		Condition: func(r *rest.Request) bool {
			return !strings.HasPrefix(r.URL.Path, "/event-types")
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	readScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.Webhooks,
		utils.Scopes.ReadWebhooks,
	}
	writeScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Webhooks,
		utils.Scopes.WriteWebhooks,
	}

	router, _ := rest.MakeRouter(
		// Public catalog.
		rest.Get("/event-types", app.handleProxy),

		// Events. These must be registered before the /#id wildcards below,
		// otherwise "/events" is swallowed by rest.Get("/#id") with
		// id="events" — which happens to forward to the right upstream
		// handler by luck, while "/events/<eid>" matches no route at all and
		// is rejected with a 404 here without ever reaching the service.
		rest.Get("/events", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Get("/events/#eid", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Get("/events/#eid/deliveries", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Post("/events/#eid/redeliver", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),

		// Subscriptions.
		rest.Get("/", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Post("/", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),
		rest.Get("/#id", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Put("/#id", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),
		rest.Patch("/#id", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),
		rest.Delete("/#id", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),
		rest.Post("/#id/rotate-secret", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),
		rest.Post("/#id/test", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),

		// Deliveries.
		rest.Get("/#id/deliveries", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Get("/#id/deliveries/#did", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(readScopes)}, app.handleProxy)),
		rest.Post("/#id/deliveries/#did/replay", rest.WrapMiddlewares(
			[]rest.Middleware{utils.InitScopeFilterMiddleware(writeScopes)}, app.handleProxy)),
	)

	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      router,
	})
	app.API.SetApp(router)
	return app
}

// handleProxy strips the inbound Authorization header, attaches the
// resolved identity as X-Pantahub-* headers, then forwards via the
// reverse proxy. The Director set in New() takes care of the URL.
func (app *App) handleProxy(w rest.ResponseWriter, r *rest.Request) {
	// Strip headers the upstream must never see.
	r.Request.Header.Del("Authorization")
	r.Request.Header.Del("Cookie")
	// Defense in depth: strip any client-supplied trust headers; we set
	// them ourselves below.
	r.Request.Header.Del("X-Pantahub-Caller")
	r.Request.Header.Del("X-Pantahub-Owner")
	r.Request.Header.Del("X-Pantahub-Type")
	r.Request.Header.Del("X-Pantahub-Scopes")
	r.Request.Header.Del("X-Pantahub-Proxy-Timestamp")
	r.Request.Header.Del("X-Pantahub-Proxy-Signature")

	authInfo := utils.GetAuthInfo(r)
	var owner, caller string
	if authInfo != nil {
		caller = string(authInfo.Caller)
		owner = string(authInfo.Owner)
		r.Request.Header.Set("X-Pantahub-Caller", caller)
		r.Request.Header.Set("X-Pantahub-Owner", owner)
		r.Request.Header.Set("X-Pantahub-Type", authInfo.CallerType)
		if len(authInfo.Scopes) > 0 {
			r.Request.Header.Set("X-Pantahub-Scopes", strings.Join(authInfo.Scopes, " "))
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
			rest.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// The body digest is part of the signed string, so the body has to
		// be buffered and handed back for the proxy to forward. These are
		// small JSON management-API payloads, never streams.
		var body []byte
		if r.Request.Body != nil {
			body, err = io.ReadAll(r.Request.Body)
			if err != nil {
				rest.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
				return
			}
			r.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
		bodySum := sha256.Sum256(body)

		// The upstream's mux strips /webhooks; sign the path the upstream's
		// inner router will see (which still includes /webhooks because
		// the upstream re-prefixes), keeping signer/verifier in agreement.
		signedPath := "/webhooks" + r.URL.Path

		// Sign the header values as actually set above, so signer and
		// verifier read identical strings even when authInfo was nil.
		base := strings.Join([]string{
			"v2", ts, nonce, r.Request.Method, signedPath,
			canonicalQuery(r.URL.RawQuery),
			hex.EncodeToString(bodySum[:]),
			r.Request.Header.Get("X-Pantahub-Owner"),
			r.Request.Header.Get("X-Pantahub-Caller"),
			r.Request.Header.Get("X-Pantahub-Type"),
			r.Request.Header.Get("X-Pantahub-Scopes"),
		}, "\n")
		mac := hmac.New(sha256.New, app.proxySecrets[0])
		mac.Write([]byte(base))
		r.Request.Header.Set("X-Pantahub-Proxy-Timestamp", ts)
		r.Request.Header.Set("X-Pantahub-Proxy-Nonce", nonce)
		r.Request.Header.Set("X-Pantahub-Proxy-Signature", "v2="+hex.EncodeToString(mac.Sum(nil)))
	}

	app.proxy.ServeHTTP(flushSafeWriter{w.(http.ResponseWriter)}, r.Request)
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
// datacounter.ResponseWriterCounter, which has no Flush method, and
// go-json-rest's responseWriter.Flush type-asserts its wrapped writer to
// http.Flusher without checking. ReverseProxy flushes from the
// maxLatencyWriter goroutine, so that panic unwinds outside the recover
// middleware and kills the whole process rather than failing one request.
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
	// go-json-rest's responseWriter satisfies the interface but its Flush
	// type-asserts its own inner writer without checking, so it panics on
	// the datacounter wrapper underneath. Recover here because this runs on
	// ReverseProxy's maxLatencyWriter goroutine, outside the reach of the
	// recover middleware, where a panic kills the process instead of the
	// request. Losing an early flush only delays bytes; the response is
	// still written in full when the handler returns.
	defer func() { _ = recover() }()
	fl.Flush()
}
