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

// Differential test: URLClean under a mount prefix, as subscriptions and
// trails use it.
func TestURLCleanMatchesGoJSONRest(t *testing.T) {
	s := NewServer("t")
	g := s.Mount("/svc", URLClean())
	eh := func(c *echo.Context) error {
		return WriteJSON(c, http.StatusOK, serviceRequest(c.Request(), "/svc").URL.Path+"|"+c.Param("id"))
	}
	g.GET("", eh)
	g.PUT("/admin/subscription", eh)
	g.GET("/things/:id", eh)
	emux := http.NewServeMux()
	emux.Handle("/svc/", s.E)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/svc/"},
		{"GET", "/svc//"},
		{"GET", "/svc"},
		{"PUT", "/svc/admin/subscription"},
		{"PUT", "/svc/admin/subscription/"},
		{"PUT", "/svc/admin/subscription//"},
		{"GET", "/svc/admin/subscription"},
		{"GET", "/svc/things/a"},
		{"GET", "/svc/things/a/"},
		{"GET", "/svc/things/a%2Fb/"},
		{"GET", "/svc/things/a%2Fb"},
		{"GET", "/svc/things/"},
		{"GET", "/svc/nope/"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			run := func(h http.Handler) result {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
				return result{rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()}
			}
			requireIdentical(t, recorded[result](t, "gjr", nil), run(emux))
		})
	}
}
