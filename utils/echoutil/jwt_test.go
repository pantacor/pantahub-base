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
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

// Differential tests for JWT: responses, challenge and claims must match.

const testKey = "differential-test-key"

// The realm production uses, including the embedded ph-aeps discovery URL.
const testRealm = `"pantahub services", ph-aeps="https://api.example.com/auth"`

func newJWT() *jwtauth.Config {
	return &jwtauth.Config{
		Realm:            testRealm,
		SigningAlgorithm: "HS256",
		Key:              []byte(testKey),
		Timeout:          time.Hour,
	}
}

func signed(t *testing.T, claims jwt.MapClaims, key string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return s
}

type authResult struct {
	result
	WWWAuth    string
	Reached    bool
	RemoteUser interface{}
	Nick       interface{}
}

func authViaEcho(t *testing.T, authHeader string) authResult {
	t.Helper()
	var out authResult

	e := echo.New()
	e.GET("/", func(c *echo.Context) error {
		out.Reached = true
		out.RemoteUser = c.Get(KeyRemoteUser)
		out.Nick = Claims(c)["nick"]
		return WriteJSON(c, http.StatusOK, map[string]string{"ok": "yes"})
	}, JWT(newJWT()))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	out.result = result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())}
	out.WWWAuth = rec.Header().Get("WWW-Authenticate")
	return out
}

func TestJWTMatchesGoJSONRest(t *testing.T) {
	valid := signed(t, jwt.MapClaims{"id": "user1", "nick": "user1nick", "exp": time.Now().Add(time.Hour).Unix()}, testKey)
	expired := signed(t, jwt.MapClaims{"id": "user1", "exp": time.Now().Add(-time.Hour).Unix()}, testKey)
	wrongKey := signed(t, jwt.MapClaims{"id": "user1", "exp": time.Now().Add(time.Hour).Unix()}, "not-the-key")

	for _, tc := range []struct {
		name, header string
		wantReached  bool
	}{
		{"no header", "", false},
		{"empty bearer", "Bearer ", false},
		{"scheme only", "Bearer", false},
		{"lowercase bearer", "bearer " + valid, false},
		{"wrong scheme", "Basic " + valid, false},
		{"garbage token", "Bearer not-a-jwt", false},
		{"wrong signing key", "Bearer " + wrongKey, false},
		{"expired", "Bearer " + expired, false},
		{"valid", "Bearer " + valid, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gjr := recorded[authResult](t, "gjr", nil)
			ech := authViaEcho(t, tc.header)

			requireIdentical(t, gjr.result, ech.result)
			if gjr.WWWAuth != ech.WWWAuth {
				t.Errorf("WWW-Authenticate:\n  go-json-rest=%q\n  echo        =%q", gjr.WWWAuth, ech.WWWAuth)
			}
			if gjr.Reached != tc.wantReached || ech.Reached != tc.wantReached {
				t.Errorf("handler reached: go-json-rest=%v echo=%v, want %v", gjr.Reached, ech.Reached, tc.wantReached)
			}
			if gjr.RemoteUser != ech.RemoteUser {
				t.Errorf("REMOTE_USER: go-json-rest=%v echo=%v", gjr.RemoteUser, ech.RemoteUser)
			}
			if gjr.Nick != ech.Nick {
				t.Errorf("claims nick: go-json-rest=%v echo=%v", gjr.Nick, ech.Nick)
			}
		})
	}
}

// Pins the literal header so a change that altered BOTH frameworks identically
// -- which the differential test cannot see -- is still caught.
func TestJWTUnauthorizedHeaderIsExact(t *testing.T) {
	got := authViaEcho(t, "").WWWAuth
	const want = `JWT realm="pantahub services", ph-aeps="https://api.example.com/auth"`
	if got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

// A token bound to an OAuth protected resource is signed with the same key as
// every other token; the audience is the only thing that keeps it out of here.
func TestJWTRefusesResourceBoundTokens(t *testing.T) {
	claims := func(aud interface{}) jwt.MapClaims {
		c := jwt.MapClaims{"id": "prn:pantahub.com:auth:/user1", "nick": "user1", "exp": time.Now().Add(time.Hour).Unix()}
		if aud != nil {
			c["aud"] = aud
		}
		return c
	}

	for name, aud := range map[string]interface{}{
		"bound to an mcp endpoint":  "https://api.example.com/mcp",
		"bound, in a list":          []string{"prn:pantahub.com:auth:/service1", "https://api.example.com/mcp"},
		"bound over plain http":     "http://localhost:12365/mcp",
		"bound, odd capitalisation": "HTTPS://api.example.com/mcp",
	} {
		got := authViaEcho(t, "Bearer "+signed(t, claims(aud), testKey))
		if got.Reached || got.Status != http.StatusUnauthorized {
			t.Errorf("%s: reached=%v status=%d, want 401 and not reached", name, got.Reached, got.Status)
		}
		if got.WWWAuth == "" {
			t.Errorf("%s: a refusal still carries the challenge", name)
		}
	}

	// The audiences in use before are PRNs and keep working.
	for name, aud := range map[string]interface{}{
		"no audience":            nil,
		"on behalf of a service": "prn:pantahub.com:auth:/service1",
	} {
		got := authViaEcho(t, "Bearer "+signed(t, claims(aud), testKey))
		if !got.Reached || got.Status != http.StatusOK {
			t.Errorf("%s: reached=%v status=%d, want 200", name, got.Reached, got.Status)
		}
	}
}
