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
	"reflect"
	"testing"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Differential tests: the handler must see the same caller identity.

type identity struct {
	Reached  bool
	AuthInfo *utils.AuthInfo
	Payload  jwt.MapClaims
	Orig     jwt.MapClaims
}

// gjrAuthResult carries recorded go-json-rest values through one
// recorded call (JSON-friendly: only exported fields).
type gjrAuthResult struct {
	Res result
	ID  identity
}

func identityViaEcho(t *testing.T, token string) (result, identity) {
	t.Helper()
	var id identity

	e := echo.New()
	e.GET("/", func(c *echo.Context) error {
		id.Reached = true
		id.AuthInfo = AuthInfo(c)
		id.Payload, _ = c.Get(KeyJWTPayload).(jwt.MapClaims)
		id.Orig, _ = c.Get(KeyJWTOrigPayload).(jwt.MapClaims)
		return WriteJSON(c, http.StatusOK, map[string]string{"ok": "yes"})
	}, JWT(newJWT()), Auth())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())}, id
}

func TestAuthMatchesGoJSONRest(t *testing.T) {
	// Fixed, far-future exp: the recorded go-json-rest payloads must replay unchanged.
	exp := int64(4102444800)

	for _, tc := range []struct {
		name        string
		claims      jwt.MapClaims
		wantReached bool
		wantInfo    *utils.AuthInfo
	}{
		{
			name: "user",
			claims: jwt.MapClaims{
				"id": "user1", "exp": exp, "prn": "prn:::accounts:/u1", "type": "USER",
				"nick": "user1", "roles": "user", "aud": "prn:pantahub.com:apis:/api",
				"scopes": "prn:pantahub.com:apis:/base/all  prn:pantahub.com:apis:/fleet/all",
				"owner":  "prn:::accounts:/u1",
			},
			wantReached: true,
			wantInfo: &utils.AuthInfo{
				Caller: "prn:::accounts:/u1", CallerType: "USER", Owner: "prn:::accounts:/u1",
				Roles: "user", Audience: "prn:pantahub.com:apis:/api", Nick: "user1",
				// Whitespace-separated, and runs of spaces collapse (strings.Fields).
				Scopes:     []string{"prn:pantahub.com:apis:/base/all", "prn:pantahub.com:apis:/fleet/all"},
				RemoteUser: "user1==>user1",
			},
		},
		{
			// A service impersonating a device. The handler must act as the
			// device, carry the service's exp, and name both in RemoteUser.
			name: "call-as impersonation",
			claims: jwt.MapClaims{
				"id": "svc", "exp": exp, "orig_iat": float64(1700000000), "prn": "prn:::accounts:/svc",
				"type": "SERVICE", "nick": "fleet-service",
				"call-as": map[string]interface{}{
					"prn": "prn:::devices:/d1", "type": "DEVICE", "nick": "device1",
					"owner": "prn:::accounts:/owner1",
				},
			},
			wantReached: true,
			wantInfo: &utils.AuthInfo{
				Caller: "prn:::devices:/d1", CallerType: "DEVICE", Owner: "prn:::accounts:/owner1",
				Nick: "device1", RemoteUser: "fleet-service==>device1",
			},
		},
		{
			name:        "no nick on the original caller",
			claims:      jwt.MapClaims{"id": "x", "exp": exp, "prn": "prn:::accounts:/x", "type": "USER"},
			wantReached: true,
			wantInfo:    &utils.AuthInfo{Caller: "prn:::accounts:/x", CallerType: "USER", RemoteUser: "_unknown_==>"},
		},
		{name: "missing prn is forbidden", claims: jwt.MapClaims{"id": "x", "exp": exp, "type": "USER"}},
		{name: "missing type is forbidden", claims: jwt.MapClaims{"id": "x", "exp": exp, "prn": "prn:::accounts:/x"}},
		{
			name: "call-as without prn is forbidden",
			claims: jwt.MapClaims{
				"id": "svc", "exp": exp, "prn": "prn:::accounts:/svc", "type": "SERVICE",
				"call-as": map[string]interface{}{"type": "DEVICE"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := signed(t, tc.claims, testKey)
			g := recorded[gjrAuthResult](t, "gjr", nil)
			gjrRes, gjrID := g.Res, g.ID
			echRes, echID := identityViaEcho(t, token)

			requireIdentical(t, gjrRes, echRes)

			if gjrID.Reached != tc.wantReached || echID.Reached != tc.wantReached {
				t.Fatalf("handler reached: go-json-rest=%v echo=%v, want %v", gjrID.Reached, echID.Reached, tc.wantReached)
			}
			if !tc.wantReached {
				if gjrRes.Status != http.StatusForbidden {
					t.Errorf("rejection status = %d, want 403", gjrRes.Status)
				}
				return
			}

			if !reflect.DeepEqual(gjrID.AuthInfo, echID.AuthInfo) {
				t.Errorf("AuthInfo differs:\n  go-json-rest=%+v\n  echo        =%+v", gjrID.AuthInfo, echID.AuthInfo)
			}
			if !reflect.DeepEqual(gjrID.AuthInfo, tc.wantInfo) {
				t.Errorf("AuthInfo = %+v\n     want %+v", gjrID.AuthInfo, tc.wantInfo)
			}
			if !reflect.DeepEqual(gjrID.Payload, echID.Payload) {
				t.Errorf("JWT_PAYLOAD differs:\n  go-json-rest=%v\n  echo        =%v", gjrID.Payload, echID.Payload)
			}
			if !reflect.DeepEqual(gjrID.Orig, echID.Orig) {
				t.Errorf("JWT_ORIG_PAYLOAD differs:\n  go-json-rest=%v\n  echo        =%v", gjrID.Orig, echID.Orig)
			}
		})
	}
}
