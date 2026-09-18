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

package exports

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

func TestBearerOrAnon(t *testing.T) {
	cfg := &jwtauth.Config{Realm: "test", Key: []byte("k")}
	cfg.ApplyDefaults()
	sign := func(id string) string {
		s, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, jwtgo.MapClaims{"id": id, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("k"))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	anon := sign("anon")

	for _, tc := range []struct {
		name, authz string
		code        int
		user        string
	}{
		{"no credentials get the anon token", "", 200, "anon"},
		{"valid bearer", "Bearer " + sign("u1"), 200, "u1"},
		{"lowercase bearer reaches JWT and is rejected", "bearer " + sign("u1"), 401, ""},
		{"invalid bearer", "Bearer nope", 401, ""},
		{"other scheme passes through unauthenticated", "Basic Zm9vOmJhcg==", 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echoutil.New()
			e.GET("/x", func(c *echo.Context) error {
				user, _ := c.Get(echoutil.KeyRemoteUser).(string)
				return c.String(http.StatusOK, user)
			}, bearerOrAnon(echoutil.JWT(cfg), func() string { return anon }))
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("code = %d, want %d", rec.Code, tc.code)
			}
			if tc.code == 200 && rec.Body.String() != tc.user {
				t.Fatalf("user = %q, want %q", rec.Body.String(), tc.user)
			}
		})
	}
}
