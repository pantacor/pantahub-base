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
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

// Differential tests for AuthBasic against go-json-rest's AuthBasicMiddleware.
// The decoder is reimplemented here, so every branch of it is exercised.

// The realm production uses: unquoted, with spaces and an "@".
const basicRealm = "Pantahub Health @ https://api.example.com/auth"

func newBasic() BasicAuthConfig {
	return BasicAuthConfig{
		Realm:         basicRealm,
		Authenticator: func(u, p string) bool { return u == "saadmin" && p == "s3cr:et" },
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

type basicAuthResult struct {
	result
	WWWAuth string
	Reached bool
	User    interface{}
}

func authBasicViaEcho(t *testing.T, authz string, setHeader bool) basicAuthResult {
	t.Helper()
	var out basicAuthResult
	e := New()
	e.PUT("/devices/:id", func(c *echo.Context) error {
		out.Reached = true
		out.User = c.Get(KeyRemoteUser)
		return WriteJSON(c, http.StatusOK, c.Param("id"))
	}, AuthBasic(newBasic()))
	req := httptest.NewRequest(http.MethodPut, "/devices/d1", nil)
	if setHeader {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	out.result = result{rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()}
	out.WWWAuth = rec.Header().Get("WWW-Authenticate")
	return out
}

func TestAuthBasicMatchesGoJSONRest(t *testing.T) {
	for _, tc := range []struct {
		name, authz string
		set         bool
	}{
		{"no header", "", false},
		{"empty header", "", true},
		{"valid (password contains a colon)", "Basic " + b64("saadmin:s3cr:et"), true},
		{"wrong password", "Basic " + b64("saadmin:nope"), true},
		{"wrong user", "Basic " + b64("root:s3cr:et"), true},
		{"lowercase scheme", "basic " + b64("saadmin:s3cr:et"), true},
		{"bearer scheme", "Bearer " + b64("saadmin:s3cr:et"), true},
		{"scheme only", "Basic", true},
		{"invalid base64", "Basic !!!", true},
		{"no colon", "Basic " + b64("saadmin"), true},
		{"empty credentials", "Basic " + b64(":"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gjr := recorded[basicAuthResult](t, "gjr", nil)
			ech := authBasicViaEcho(t, tc.authz, tc.set)
			requireIdentical(t, gjr.result, ech.result)
			if gjr.WWWAuth != ech.WWWAuth {
				t.Errorf("WWW-Authenticate: go-json-rest=%q echo=%q", gjr.WWWAuth, ech.WWWAuth)
			}
			if gjr.Reached != ech.Reached || gjr.User != ech.User {
				t.Errorf("reached/user: go-json-rest=(%v,%v) echo=(%v,%v)", gjr.Reached, gjr.User, ech.Reached, ech.User)
			}
		})
	}
}

// Pins the literal challenge, unquoted realm and all, so a change applied to
// both frameworks identically is still caught.
func TestAuthBasicChallengeIsExact(t *testing.T) {
	got := authBasicViaEcho(t, "", false).WWWAuth
	const want = "Basic realm=Pantahub Health @ https://api.example.com/auth"
	if got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}
