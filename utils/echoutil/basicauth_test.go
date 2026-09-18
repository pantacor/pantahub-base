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
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

// Differential tests for BasicAuthToBearer stacked before JWT.

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// stubFactory accepts exactly user1:good-token and mints a bearer the test
// middleware will verify.
func stubFactory(t *testing.T) func(context.Context, string, string, *jwtauth.Config, *mongo.Client, time.Duration) (string, *utils.RError) {
	return func(_ context.Context, user, pass string, _ *jwtauth.Config, _ *mongo.Client, ttl time.Duration) (string, *utils.RError) {
		if user != "user1" || pass != "good-token" {
			return "", &utils.RError{Error: "bad personal token", Code: http.StatusUnauthorized}
		}
		return signed(t, jwt.MapClaims{"id": user, "nick": user, "exp": time.Now().Add(ttl).Unix()}, testKey), nil
	}
}

type basicResult struct {
	result
	WWWAuth   string
	Reached   bool
	BasicUser interface{}
	Remote    interface{}
}

func basicViaEcho(t *testing.T, authz string) basicResult {
	t.Helper()
	var out basicResult

	e := echo.New()
	e.GET("/", func(c *echo.Context) error {
		out.Reached = true
		out.BasicUser = c.Get(KeyBasicAuthUser)
		out.Remote = c.Get(KeyRemoteUser)
		return WriteJSON(c, http.StatusOK, map[string]string{"ok": "yes"})
	}, BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: newJWT()}), JWT(newJWT()))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	out.result = result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())}
	out.WWWAuth = rec.Header().Get("WWW-Authenticate")
	return out
}

func TestBasicAuthToBearerMatchesGoJSONRest(t *testing.T) {
	prev := utils.BasicAuthTokenFactory
	t.Cleanup(func() { utils.BasicAuthTokenFactory = prev })

	for _, tc := range []struct {
		name, authz string
		factory     bool
		wantReached bool
		wantWWWAuth string
	}{
		{name: "valid personal token", authz: basic("user1", "good-token"), factory: true, wantReached: true},
		// Rejected Basic answers with its OWN challenge, not the JWT one.
		{name: "wrong personal token", authz: basic("user1", "account-password"), factory: true, wantWWWAuth: utils.BasicAuthChallenge},
		{name: "no header", factory: true, wantWWWAuth: "JWT realm=" + testRealm},
		{name: "malformed basic", authz: "Basic !!!not-base64", factory: true, wantWWWAuth: "JWT realm=" + testRealm},
		{name: "empty username", authz: basic("", "good-token"), factory: true, wantWWWAuth: "JWT realm=" + testRealm},
		{name: "no factory configured", authz: basic("user1", "good-token"), wantWWWAuth: "JWT realm=" + testRealm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			utils.BasicAuthTokenFactory = nil
			if tc.factory {
				utils.BasicAuthTokenFactory = stubFactory(t)
			}

			gjr := recorded[basicResult](t, "gjr", nil)
			ech := basicViaEcho(t, tc.authz)

			requireIdentical(t, gjr.result, ech.result)
			if gjr.WWWAuth != ech.WWWAuth {
				t.Errorf("WWW-Authenticate:\n  go-json-rest=%q\n  echo        =%q", gjr.WWWAuth, ech.WWWAuth)
			}
			if gjr.WWWAuth != tc.wantWWWAuth {
				t.Errorf("WWW-Authenticate = %q, want %q", gjr.WWWAuth, tc.wantWWWAuth)
			}
			if gjr.Reached != tc.wantReached || ech.Reached != tc.wantReached {
				t.Errorf("handler reached: go-json-rest=%v echo=%v, want %v", gjr.Reached, ech.Reached, tc.wantReached)
			}
			if gjr.BasicUser != ech.BasicUser || gjr.Remote != ech.Remote {
				t.Errorf("identity: go-json-rest=(%v,%v) echo=(%v,%v)", gjr.BasicUser, gjr.Remote, ech.BasicUser, ech.Remote)
			}
		})
	}
}
