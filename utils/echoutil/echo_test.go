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

// Differential tests for routing edge cases and panics.

type routeResult struct {
	result
	Allow string
}

func routeViaEcho(t *testing.T, method, path string) routeResult {
	t.Helper()
	e := New()
	e.Pre(Recover())
	e.GET("/things", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, []string{"a"}) })
	e.GET("/things/:id", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, c.Param("id")) })
	e.POST("/things", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, "created") })
	e.GET("/boom", func(c *echo.Context) error { panic("handler exploded") })
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return routeResult{result{rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()}, rec.Header().Get("Allow")}
}

func TestRoutingEdgeCasesMatchGoJSONRest(t *testing.T) {
	for _, tc := range []struct{ name, method, path string }{
		{"match", "GET", "/things"},
		{"path parameter", "GET", "/things/abc123"},
		{"second method on same path", "POST", "/things"},
		{"unknown path", "GET", "/nope"},
		// A trailing :param must not swallow further segments.
		{"unknown nested path", "GET", "/things/abc/extra"},
		{"param swallowing several segments", "GET", "/things/a/b/c"},
		{"param with trailing slash", "GET", "/things/abc/"},
		{"encoded slash in param passes through", "GET", "/things/abc%2Fextra"},
		{"encoded traversal in param passes through", "GET", "/things/..%2F..%2Fsecret"},
		{"known path, wrong method", "DELETE", "/things"},
		{"known param path, wrong method", "PUT", "/things/abc123"},
		// echo's router hardcodes 204 for this; go-json-rest answers 405.
		{"OPTIONS without a route", "OPTIONS", "/things"},
		{"trailing slash is a different path", "GET", "/things/"},
		{"handler panic", "GET", "/boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gjr := recorded[routeResult](t, "gjr", nil)
			ech := routeViaEcho(t, tc.method, tc.path)
			requireIdentical(t, gjr.result, ech.result)
			if gjr.Allow != ech.Allow {
				t.Errorf("Allow header: go-json-rest=%q echo=%q", gjr.Allow, ech.Allow)
			}
		})
	}
}

// A handler that returns an error instead of answering has no go-json-rest
// equivalent. It must get the generic 500, never err.Error() on the wire.
func TestReturnedErrorDoesNotLeak(t *testing.T) {
	e := New()
	e.GET("/x", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "mongo: dial tcp 10.0.0.5:27017")
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	const want = `{"Error":"Internal Server Error"}`
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != want {
		t.Fatalf("got %d %q, want 500 %q", rec.Code, rec.Body.String(), want)
	}
}
