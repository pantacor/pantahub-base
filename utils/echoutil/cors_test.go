// Copyright (c) 2017-2026 Pantacor Ltd.
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

package echoutil

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/labstack/echo/v5"
)

// Differential tests for CORS: status, body and Access-Control-* headers.

// newCors returns a fresh config per stack.
func newCors() CORSConfig {
	return CORSConfig{
		RejectNonCorsRequests:         false,
		OriginValidator:               AllowAllOrigins,
		AllowedMethods:                []string{"GET", "post", "OPTIONS"},
		AllowedHeaders:                []string{"accept", "Content-Type", "authorization", "x-request-id"},
		AccessControlExposeHeaders:    []string{"X-Custom-Exposed"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	}
}

type corsResult struct {
	Status  int
	Body    string
	Headers map[string][]string
}

func corsHeaders(h http.Header) map[string][]string {
	out := map[string][]string{}
	for k, v := range h {
		if len(k) > 14 && k[:14] == "Access-Control" || k == "Content-Type" {
			out[k] = v
		}
	}
	return out
}

func corsViaEcho(t *testing.T, req *http.Request) corsResult {
	t.Helper()
	e := New()             // go-json-rest-compatible routing, so non-CORS OPTIONS gets the same 405
	e.Pre(CORS(newCors())) // before routing, as go-json-rest did it
	e.GET("/thing", func(c *echo.Context) error {
		return WriteJSON(c, http.StatusOK, map[string]string{"ok": "yes"})
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return corsResult{rec.Code, rec.Body.String(), corsHeaders(rec.Header())}
}

func TestCORSMatchesGoJSONRest(t *testing.T) {
	type reqSpec struct {
		method  string
		host    string
		headers map[string]string
		rawACRH []string // multiple Access-Control-Request-Headers header lines
	}
	for _, tc := range []struct {
		name string
		req  reqSpec
	}{
		{"not CORS: no Origin", reqSpec{method: "GET"}},
		{"not CORS: Origin on the same host", reqSpec{method: "GET", host: "api.example.com",
			headers: map[string]string{"Origin": "https://api.example.com"}}},
		{"CORS simple GET", reqSpec{method: "GET", headers: map[string]string{"Origin": "https://hub.example.com"}}},
		{"CORS Origin null", reqSpec{method: "GET", headers: map[string]string{"Origin": "null"}}},
		{"preflight allowed", reqSpec{method: "OPTIONS", headers: map[string]string{
			"Origin": "https://hub.example.com", "Access-Control-Request-Method": "GET",
			"Access-Control-Request-Headers": "authorization, content-type"}}},
		// POST is on the list in lower case; normalisation must accept it.
		{"preflight method normalised", reqSpec{method: "OPTIONS", headers: map[string]string{
			"Origin": "https://hub.example.com", "Access-Control-Request-Method": "post"}}},
		// A route registered only for GET: CORS must answer before any 405.
		{"preflight for a GET-only route with PUT", reqSpec{method: "OPTIONS", headers: map[string]string{
			"Origin": "https://hub.example.com", "Access-Control-Request-Method": "PUT"}}},
		{"preflight header outside the list is rejected", reqSpec{method: "OPTIONS", headers: map[string]string{
			"Origin": "https://hub.example.com", "Access-Control-Request-Method": "GET",
			"Access-Control-Request-Headers": "traceparent"}}},
		{"preflight headers split across lines", reqSpec{method: "OPTIONS",
			headers: map[string]string{"Origin": "https://hub.example.com", "Access-Control-Request-Method": "GET"},
			rawACRH: []string{"accept", "X-REQUEST-ID, content-type", ""}}},
		{"OPTIONS without request method is not a preflight", reqSpec{method: "OPTIONS",
			headers: map[string]string{"Origin": "https://hub.example.com"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			build := func() *http.Request {
				r := httptest.NewRequest(tc.req.method, "http://api.example.com/thing", nil)
				if tc.req.host != "" {
					r.Host = tc.req.host
				}
				for k, v := range tc.req.headers {
					r.Header.Set(k, v)
				}
				for _, v := range tc.req.rawACRH {
					r.Header.Add("Access-Control-Request-Headers", v)
				}
				return r
			}

			gjr := recorded[corsResult](t, "gjr", nil)
			ech := corsViaEcho(t, build())

			if gjr.Status != ech.Status {
				t.Errorf("status: go-json-rest=%d echo=%d", gjr.Status, ech.Status)
			}
			if gjr.Body != ech.Body {
				t.Errorf("body:\n  go-json-rest=%q\n  echo        =%q", gjr.Body, ech.Body)
			}
			if !reflect.DeepEqual(gjr.Headers, ech.Headers) {
				t.Errorf("CORS headers:\n  go-json-rest=%v\n  echo        =%v", gjr.Headers, ech.Headers)
			}
		})
	}
}
