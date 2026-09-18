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
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// End-to-end: two services (different CORS) on one Server, against the
// recorded go-json-rest+StripPrefix answers.

func corsFor(headers ...string) CORSConfig {
	return CORSConfig{
		OriginValidator:               AllowAllOrigins,
		AllowedMethods:                []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:                headers,
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	}
}

// Service "alpha" allows the Authorization header in preflights; "beta" does not.
func alphaCors() CORSConfig { return corsFor("Authorization", "Content-Type") }
func betaCors() CORSConfig  { return corsFor("Content-Type") }

func echoMux(t *testing.T) http.Handler {
	s := NewServer("test")
	chain := func(prefix string, cors CORSConfig) []echo.MiddlewareFunc {
		return []echo.MiddlewareFunc{
			AccessLogJSON(log.New(&bytes.Buffer{}, "", 0), prefix),
			Instrument(), Recover(), CORS(cors),
			BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: newJWT()}),
			JWT(newJWT()), Auth(),
		}
	}
	alpha := s.Mount("/alpha", chain("/alpha", alphaCors())...)
	alpha.GET("/", func(c *echo.Context) error {
		return WriteJSON(c, http.StatusOK, map[string]interface{}{"svc": "alpha", "caller": AuthInfo(c).Caller})
	})
	alpha.GET("/items/:id", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, c.Param("id")) })
	beta := s.Mount("/beta", chain("/beta", betaCors())...)
	beta.GET("/", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, "beta") })

	mux := http.NewServeMux()
	mux.Handle("/alpha/", s.E)
	mux.Handle("/beta/", s.E)
	return mux
}

// httpResult bundles the three values one gjr/echo run is compared on.
type httpResult struct {
	Result  result // result round-trips through the golden JSON.
	WWWAuth string
	ACAH    string
}

func TestServerMountMatchesGoJSONRest(t *testing.T) {
	token := signed(t, map[string]interface{}{
		"id": "u1", "prn": "prn:::accounts:/u1", "type": "USER", "nick": "u1", "exp": float64(4102444800),
	}, testKey)
	auth := map[string]string{"Authorization": "Bearer " + token}
	preflight := func(reqHeaders string) map[string]string {
		return map[string]string{"Origin": "https://hub.example.com", "Access-Control-Request-Method": "GET",
			"Access-Control-Request-Headers": reqHeaders}
	}

	for _, tc := range []struct {
		name, method, path string
		headers            map[string]string
	}{
		{"alpha authenticated", "GET", "/alpha/", auth},
		{"alpha path param", "GET", "/alpha/items/x1", auth},
		// go-json-rest authenticates BEFORE routing: no credentials to a route
		// that does not exist is 401, not 404.
		{"unauthenticated, route exists", "GET", "/alpha/", nil},
		{"unauthenticated, route does not exist", "GET", "/alpha/does-not-exist", nil},
		{"authenticated, route does not exist", "GET", "/alpha/does-not-exist", auth},
		{"authenticated, wrong method", "DELETE", "/alpha/", auth},
		{"param swallowing is still refused through the mount", "GET", "/alpha/items/a/b", auth},
		// Each service keeps its own CORS: Authorization is allowed on alpha only.
		{"alpha preflight allows Authorization", "OPTIONS", "/alpha/", preflight("authorization")},
		{"beta preflight rejects Authorization", "OPTIONS", "/beta/", preflight("authorization")},
		{"preflight to a route that does not exist", "OPTIONS", "/alpha/nope", preflight("content-type")},
		{"beta authenticated", "GET", "/beta/", auth},
		// The mux's own subtree redirect is untouched by either framework.
		{"mux redirect without trailing slash", "GET", "/beta", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(h http.Handler) httpResult {
				req := httptest.NewRequest(tc.method, "http://api.example.com"+tc.path, nil)
				for k, v := range tc.headers {
					req.Header.Set(k, v)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return httpResult{result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())},
					rec.Header().Get("WWW-Authenticate"), rec.Header().Get("Access-Control-Allow-Headers")}
			}
			gjr := recorded[httpResult](t, "gjr", nil)
			eRes := run(echoMux(t))

			requireIdentical(t, gjr.Result, eRes.Result)
			if gjr.WWWAuth != eRes.WWWAuth {
				t.Errorf("WWW-Authenticate: go-json-rest=%q echo=%q", gjr.WWWAuth, eRes.WWWAuth)
			}
			if gjr.ACAH != eRes.ACAH {
				t.Errorf("Access-Control-Allow-Headers: go-json-rest=%q echo=%q", gjr.ACAH, eRes.ACAH)
			}
		})
	}
}

func TestValidateRejectsGoJSONRestParamSyntax(t *testing.T) {
	s := NewServer("test")
	g := s.Mount("/svc")
	g.GET("/ok/:id", func(c *echo.Context) error { return nil })
	if err := s.Validate(); err != nil {
		t.Fatalf("valid routes rejected: %v", err)
	}
	g.PUT("/devices/#id", func(c *echo.Context) error { return nil })
	if err := s.Validate(); err == nil {
		t.Fatal("route with #param was accepted")
	}
}
