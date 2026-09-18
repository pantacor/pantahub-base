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
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

// ifResult wraps the response and WWW-Authenticate header of one recorded
// go-json-rest request so it round-trips through the golden file.
type ifResult struct {
	Result  result
	WWWAuth string
}

// Differential test: conditional JWT under a mount prefix, with a whitelist
// predicate on the stripped path (as auth and webhooks use).
func TestIfMatchesGoJSONRest(t *testing.T) {
	whitelist := func(r *http.Request) bool {
		return !(r.URL.Path == "/login" && r.Method == http.MethodPost) && !strings.HasPrefix(r.URL.Path, "/event-types")
	}
	token := signed(t, map[string]interface{}{"id": "u1", "exp": float64(4102444800)}, testKey)

	for _, tc := range []struct{ name, method, path, token string }{
		{"whitelisted, no auth", "POST", "/svc/login", ""},
		{"whitelisted prefix, no auth", "GET", "/svc/event-types/x", ""},
		{"protected, no auth", "GET", "/svc/private", ""},
		{"protected, authed", "GET", "/svc/private", token},
		{"same path other method, no auth", "GET", "/svc/login", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer("t")
			eh := func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, "ok") }
			g := s.Mount("/svc", If("/svc", whitelist, JWT(newJWT())))
			g.POST("/login", eh)
			g.GET("/login", eh)
			g.GET("/private", eh)
			g.GET("/event-types/:x", eh)
			emux := http.NewServeMux()
			emux.Handle("/svc/", s.E)

			run := func(h http.Handler) (result, string) {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return result{rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()}, rec.Header().Get("WWW-Authenticate")
			}
			gjr := recorded[ifResult](t, "gjr", nil)
			er, ew := run(emux)
			requireIdentical(t, gjr.Result, er)
			if gjr.WWWAuth != ew {
				t.Errorf("WWW-Authenticate: go-json-rest=%q echo=%q", gjr.WWWAuth, ew)
			}
		})
	}
}
