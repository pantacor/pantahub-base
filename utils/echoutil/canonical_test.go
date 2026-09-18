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
	"testing"

	"github.com/labstack/echo/v5"
)

// canonicalResult wraps the response and full header of one gjr request so it
// round-trips through the recorded golden file.
type canonicalResult struct {
	Result result
	Header http.Header
}

// Differential test: CanonicalJSON ahead of CORS and JWT, as trails stacks it.
func TestCanonicalJSONMatchesGoJSONRest(t *testing.T) {
	payload := map[string]interface{}{"z": 1.5e21, "a": "é<&>", "m": []interface{}{3, "x", nil}, "n": 0.000001}
	token := signed(t, map[string]interface{}{"id": "u1", "exp": float64(4102444800)}, testKey)
	cors := func() CORSConfig {
		return CORSConfig{
			OriginValidator:               AllowAllOrigins,
			AllowedMethods:                []string{"GET", "POST"},
			AllowedHeaders:                []string{"Authorization", "Content-Type"},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}
	}

	s := NewServer("t")
	g := s.Mount("/svc", CanonicalJSON(), Instrument(), Recover(), CORS(cors()), JWT(newJWT()))
	g.GET("/json", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, payload) })
	g.GET("/conflict", func(c *echo.Context) error {
		c.Response().Header().Add("X-Pantahub-Object-Type", "object")
		return WriteHeader(c, http.StatusConflict)
	})
	g.GET("/redirect", func(c *echo.Context) error {
		c.Response().Header().Add("Location", "https://example.com/x")
		return WriteHeader(c, http.StatusFound)
	})
	g.GET("/error", func(c *echo.Context) error { return RestErrorWrapper(c, "nope", http.StatusBadRequest) })
	g.GET("/panic", func(c *echo.Context) error { panic("boom") })
	emux := http.NewServeMux()
	emux.Handle("/svc/", s.E)

	for _, tc := range []struct {
		name, method, path string
		authed, preflight  bool
	}{
		{"json", "GET", "/svc/json", true, false},
		{"409 without body", "GET", "/svc/conflict", true, false},
		{"302 redirect", "GET", "/svc/redirect", true, false},
		{"rest error", "GET", "/svc/error", true, false},
		{"panic 500", "GET", "/svc/panic", true, false},
		{"unauthenticated 401", "GET", "/svc/json", false, false},
		{"unknown route 404", "GET", "/svc/nope", true, false},
		{"wrong method 405", "POST", "/svc/json", true, false},
		{"cors preflight", "OPTIONS", "/svc/json", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(h http.Handler) (result, http.Header) {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.authed {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				if tc.preflight {
					req.Header.Set("Origin", "https://ui.example.com")
					req.Header.Set("Access-Control-Request-Method", "GET")
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				body := rec.Body.String()
				if tc.name == "rest error" {
					body = mask(body)
				}
				return result{rec.Code, rec.Header().Get("Content-Type"), body}, rec.Header()
			}
			gjr := recorded[canonicalResult](t, "gjr", nil)
			er, eh := run(emux)
			requireIdentical(t, gjr.Result, er)
			for _, k := range []string{"PhJsonFormat", "Location", "X-Pantahub-Object-Type", "Www-Authenticate", "Access-Control-Allow-Origin"} {
				if g, e := gjr.Header.Values(k), eh.Values(k); len(g) != len(e) || (len(g) > 0 && g[0] != e[0]) {
					t.Errorf("%s: go-json-rest=%q echo=%q", k, g, e)
				}
			}
		})
	}
}
