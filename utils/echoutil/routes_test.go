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

// Differential routing test over the devices and trails route tables: every
// request must resolve to the same route (or the same 404/405) on both
// routers, including paths several routes could match.

type route struct{ method, path string }

var devicesRoutes = []route{
	{"POST", "/register"}, {"POST", "/#id/ownership/validate"},
	{"POST", "/tokens"}, {"DELETE", "/tokens/#id"}, {"PATCH", "/tokens/#id"}, {"GET", "/tokens/#id"}, {"GET", "/tokens"},
	{"GET", "/auth_status"}, {"GET", "/"}, {"POST", "/"},
	{"GET", "/#id"}, {"PUT", "/#id"}, {"PATCH", "/#id"},
	{"PUT", "/#id/public"}, {"DELETE", "/#id/public"},
	{"GET", "/#id/user-meta"}, {"PUT", "/#id/user-meta"}, {"PATCH", "/#id/user-meta"},
	{"PUT", "/#id/device-meta"}, {"PATCH", "/#id/device-meta"},
	{"DELETE", "/#id"}, {"GET", "/np/#usernick/#devicenick"},
}

var trailsRoutes = []route{
	{"GET", "/auth_status"}, {"GET", "/"}, {"POST", "/"}, {"GET", "/summary"},
	{"GET", "/#id"}, {"GET", "/#id/.pvrremote"}, {"GET", "/#id/steps"}, {"POST", "/#id/steps"},
	{"GET", "/#id/steps/#rev"}, {"GET", "/#id/steps/#rev/.pvrremote"}, {"GET", "/#id/steps/#rev/meta"},
	{"GET", "/#id/steps/#rev/state"}, {"GET", "/#id/steps/#rev/objects"}, {"GET", "/#id/steps/#rev/objects/#obj"},
	{"GET", "/#id/steps/#rev/objects/#obj/blob"}, {"POST", "/#id/steps/#rev/objects"},
	{"PUT", "/#id/steps/#rev/meta"}, {"PUT", "/#id/steps/#rev/state"}, {"PUT", "/#id/steps/#rev/progress"},
	{"PUT", "/#id/steps/#rev/cancel"}, {"PUT", "/#id/steps/#rev/wontgo"}, {"GET", "/#id/summary"},
}

func compareRouting(t *testing.T, routes []route, requests []route) {
	t.Helper()
	name := func(r route) string { return r.method + " " + r.path }

	s := NewServer("t")
	g := s.Mount("/svc")
	for _, r := range routes {
		r := r
		g.Add(r.method, strings.ReplaceAll(r.path, "#", ":"), func(c *echo.Context) error {
			return WriteJSON(c, http.StatusOK, name(r)+" "+strings.Join(pathValues(c), ","))
		})
	}
	emux := http.NewServeMux()
	emux.Handle("/svc/", s.E)

	for _, rq := range requests {
		t.Run(name(rq), func(t *testing.T) {
			run := func(h http.Handler) result {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(rq.method, "/svc"+rq.path, nil))
				return result{rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()}
			}
			requireIdentical(t, recorded[result](t, "gjr", nil), run(emux))
		})
	}
}

func TestDevicesRoutingMatchesGoJSONRest(t *testing.T) {
	var reqs []route
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		for _, p := range []string{
			"/", "/register", "/tokens", "/tokens/", "/tokens/t1", "/tokens/public", "/tokens/user-meta",
			"/tokens/device-meta", "/tokens/ownership/validate", "/auth_status", "/auth_status/public",
			"/d1", "/d1/", "/d1/public", "/d1/user-meta", "/d1/device-meta", "/d1/ownership/validate",
			"/register/public", "/register/ownership/validate", "/np", "/np/u1", "/np/u1/d1", "/np/user-meta",
			"/np/public", "/np/u1/d1/x", "/d1/nope", "/a/b/c/d", "/tokens/t1/public",
		} {
			reqs = append(reqs, route{m, p})
		}
	}
	compareRouting(t, devicesRoutes, reqs)
}

func TestTrailsRoutingMatchesGoJSONRest(t *testing.T) {
	var reqs []route
	for _, m := range []string{"GET", "POST", "PUT", "DELETE"} {
		for _, p := range []string{
			"/", "/summary", "/auth_status", "/t1", "/t1/.pvrremote", "/t1/steps", "/t1/summary",
			"/summary/steps", "/summary/summary", "/auth_status/steps/1", "/t1/steps/1", "/t1/steps/1/.pvrremote",
			"/t1/steps/1/meta", "/t1/steps/1/state", "/t1/steps/1/objects", "/t1/steps/1/objects/o1",
			"/t1/steps/1/objects/o1/blob", "/t1/steps/1/progress", "/t1/steps/1/cancel", "/t1/steps/1/wontgo",
			"/t1/steps/meta", "/t1/steps/1/objects/o1/blob/x", "/t1/nope", "/t1/steps/objects/objects",
		} {
			reqs = append(reqs, route{m, p})
		}
	}
	compareRouting(t, trailsRoutes, reqs)
}

// pathValues is v4's c.ParamValues(): the matched path parameters, in order.
func pathValues(c *echo.Context) []string {
	values := make([]string, 0, len(c.PathValues()))
	for _, pv := range c.PathValues() {
		values = append(values, pv.Value)
	}
	return values
}
