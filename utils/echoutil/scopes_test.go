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
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Differential tests for ScopeFilter / ScopeFilterOptionalAuth, stacked after
// JWT and Auth as services use them.

// gjrScopeResult carries recorded go-json-rest values through one recorded
// call; result round-trips through the golden JSON.
type gjrScopeResult struct {
	Res result
	WWW string
}

func scopeRun(t *testing.T, token string, optional bool, useAuth bool) (result, result, string, string) {
	t.Helper()
	required := []utils.Scope{utils.Scopes.ReadDevices}

	echH := func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, "ok") }

	var eH echo.HandlerFunc
	if optional {
		eH = ScopeFilterOptionalAuth(required, echH)
	} else {
		eH = ScopeFilter(required, echH)
	}

	e := New()
	if useAuth {
		e.GET("/", eH, JWT(newJWT()), Auth())
	} else {
		e.GET("/", eH)
	}

	do := func(h http.Handler) (result, string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())}, rec.Header().Get("WWW-Authenticate")
	}
	gj := recorded[gjrScopeResult](t, "gjr", nil)
	g, gw := gj.Res, gj.WWW
	x, xw := do(e)
	return g, x, gw, xw
}

func TestScopeFiltersMatchGoJSONRest(t *testing.T) {
	t.Setenv("PANTAHUB_AUTH", "https://api.example.com/auth")
	base := map[string]interface{}{"id": "u1", "prn": "prn:::accounts:/u1", "type": "USER", "nick": "u1", "exp": float64(4102444800)}
	with := func(scopes string) string {
		c := map[string]interface{}{}
		for k, v := range base {
			c[k] = v
		}
		if scopes != "" {
			c["scopes"] = scopes
		}
		return signed(t, c, testKey)
	}
	readDevices := utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadDevices})[0]
	all := utils.MarshalScopes([]utils.Scope{utils.Scopes.API})[0]

	for _, tc := range []struct {
		name     string
		token    string
		optional bool
		useAuth  bool
	}{
		{"required scope present", with(readDevices), false, true},
		{"umbrella scope", with(all), false, true},
		{"insufficient scope", with("prn:pantahub.com:apis:/base/other"), false, true},
		{"no scopes claim", with(""), false, true},
		{"no resolved caller", "", false, false},
		{"optional: anonymous passes", "", true, false},
		{"optional: insufficient scope", with("prn:pantahub.com:apis:/base/other"), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, x, gw, xw := scopeRun(t, tc.token, tc.optional, tc.useAuth)
			requireIdentical(t, g, x)
			if gw != xw {
				t.Errorf("WWW-Authenticate:\n  go-json-rest=%q\n  echo        =%q", gw, xw)
			}
		})
	}
}
