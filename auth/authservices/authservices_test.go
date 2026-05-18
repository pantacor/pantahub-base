//
// Copyright 2024  Pantacor Ltd.
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

package authservices

import (
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
)

func TestCreateBearerFromPersonalToken_MalformedBase64(t *testing.T) {
	// Not valid base64.
	_, rerr := CreateBearerFromPersonalToken(nil, "user", "!!!not-base64!!!", nil, nil, time.Minute)
	if rerr == nil {
		t.Fatal("expected error for malformed base64")
	}
	if rerr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want %d", rerr.Code, http.StatusUnauthorized)
	}
}

func TestCreateBearerFromPersonalToken_NoColon(t *testing.T) {
	// Valid base64 but no colon inside.
	input := base64.RawURLEncoding.EncodeToString([]byte("nocolonhere"))
	_, rerr := CreateBearerFromPersonalToken(nil, "user", input, nil, nil, time.Minute)
	if rerr == nil {
		t.Fatal("expected error for missing colon")
	}
	if rerr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want %d", rerr.Code, http.StatusUnauthorized)
	}
}

func TestCreateBearerFromPersonalToken_NilMiddleware(t *testing.T) {
	// Correctly formatted base64 tokenid:secret, but nil middleware and mongo.
	input := base64.RawURLEncoding.EncodeToString([]byte("tokenid:secret"))
	_, rerr := CreateBearerFromPersonalToken(nil, "user", input, nil, nil, time.Minute)
	if rerr == nil {
		t.Fatal("expected error for nil middleware")
	}
	if rerr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want %d", rerr.Code, http.StatusUnauthorized)
	}
}

func TestCreateBearerFromPersonalToken_NilMongo(t *testing.T) {
	input := base64.RawURLEncoding.EncodeToString([]byte("tokenid:secret"))
	jwtMiddleware := &jwt.JWTMiddleware{SigningAlgorithm: "RS256"}
	_, rerr := CreateBearerFromPersonalToken(nil, "user", input, jwtMiddleware, nil, time.Minute)
	if rerr == nil {
		t.Fatal("expected error for nil mongoClient")
	}
	if rerr.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want %d", rerr.Code, http.StatusUnauthorized)
	}
}

// TODO: add Mongo-backed tests for:
//   - happy path with valid personal token
//   - owner mismatch
//   - expired / deleted token
//   - secret mismatch
// These require a running MongoDB or a mocked token repository.
