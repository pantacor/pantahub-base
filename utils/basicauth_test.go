//
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
//

package utils

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

func basicRequest(authz string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	return r
}

func withFactory(t *testing.T, f func(ctx context.Context, username, password string, jwtConfig *jwtauth.Config, mongoClient *mongo.Client, ttl time.Duration) (string, *RError)) {
	t.Helper()
	orig := BasicAuthTokenFactory
	BasicAuthTokenFactory = f
	t.Cleanup(func() { BasicAuthTokenFactory = orig })
}

func TestTranslate_NoAuth(t *testing.T) {
	r := basicRequest("")
	res, user := (&BasicAuthToBearerMiddleware{}).Translate(r)
	if res != BasicAuthPassThrough || user != "" || r.Header.Get("Authorization") != "" {
		t.Errorf("got %v %q %q", res, user, r.Header.Get("Authorization"))
	}
}

func TestTranslate_BearerPassThrough(t *testing.T) {
	r := basicRequest("Bearer some-token")
	res, _ := (&BasicAuthToBearerMiddleware{}).Translate(r)
	if res != BasicAuthPassThrough || r.Header.Get("Authorization") != "Bearer some-token" {
		t.Errorf("got %v, header %q", res, r.Header.Get("Authorization"))
	}
}

func TestTranslate_MalformedBasic(t *testing.T) {
	withFactory(t, func(context.Context, string, string, *jwtauth.Config, *mongo.Client, time.Duration) (string, *RError) {
		t.Fatal("factory must not be called for malformed Basic")
		return "", nil
	})
	for _, h := range []string{"Basic ", "Basic !!!", "Basic " + base64.StdEncoding.EncodeToString([]byte(":secret"))} {
		if res, _ := (&BasicAuthToBearerMiddleware{}).Translate(basicRequest(h)); res != BasicAuthPassThrough {
			t.Errorf("%q: got %v, want pass-through", h, res)
		}
	}
}

func TestTranslate_ValidBasic(t *testing.T) {
	withFactory(t, func(ctx context.Context, username, password string, jwtConfig *jwtauth.Config, mongoClient *mongo.Client, ttl time.Duration) (string, *RError) {
		if username == "alice" && password == "secret" {
			return "mock-jwt", nil
		}
		return "", &RError{Code: http.StatusUnauthorized}
	})
	r := basicRequest("Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret")))
	res, user := (&BasicAuthToBearerMiddleware{}).Translate(r)
	if res != BasicAuthRewritten || user != "alice" {
		t.Fatalf("got %v %q", res, user)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer mock-jwt" {
		t.Errorf("Authorization = %q, want \"Bearer mock-jwt\"", got)
	}
}

func TestTranslate_InvalidBasic(t *testing.T) {
	withFactory(t, func(context.Context, string, string, *jwtauth.Config, *mongo.Client, time.Duration) (string, *RError) {
		return "", &RError{Code: http.StatusUnauthorized}
	})
	r := basicRequest("Basic " + base64.StdEncoding.EncodeToString([]byte("bob:wrong")))
	if res, _ := (&BasicAuthToBearerMiddleware{}).Translate(r); res != BasicAuthRejected {
		t.Errorf("got %v, want rejected", res)
	}
}

func TestTranslate_FactoryNil(t *testing.T) {
	withFactory(t, nil)
	r := basicRequest("Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret")))
	if res, _ := (&BasicAuthToBearerMiddleware{}).Translate(r); res != BasicAuthPassThrough {
		t.Errorf("got %v, want pass-through", res)
	}
}
