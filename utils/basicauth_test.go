//
// Copyright 2026 Pantacor Ltd.
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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestBasicAuthToBearerMiddleware_NoAuth(t *testing.T) {
	mw := &BasicAuthToBearerMiddleware{}
	handlerCalled := false
	handler := func(w rest.ResponseWriter, r *rest.Request) {
		handlerCalled = true
	}

	r := buildRestRequest(t, "")
	mw.MiddlewareFunc(handler)(r.responseWriter, r.req)
	if !handlerCalled {
		t.Error("handler should have been called")
	}
	if r.req.Header.Get("Authorization") != "" {
		t.Error("Authorization header should remain empty")
	}
}

func TestBasicAuthToBearerMiddleware_BearerPassThrough(t *testing.T) {
	mw := &BasicAuthToBearerMiddleware{}
	handlerCalled := false
	handler := func(w rest.ResponseWriter, r *rest.Request) {
		handlerCalled = true
	}

	r := buildRestRequest(t, "Bearer some-token")
	mw.MiddlewareFunc(handler)(r.responseWriter, r.req)
	if !handlerCalled {
		t.Error("handler should have been called")
	}
	if r.req.Header.Get("Authorization") != "Bearer some-token" {
		t.Error("Authorization header should remain unchanged")
	}
}

func TestBasicAuthToBearerMiddleware_MalformedBasic(t *testing.T) {
	mw := &BasicAuthToBearerMiddleware{}
	handlerCalled := false
	handler := func(w rest.ResponseWriter, r *rest.Request) {
		handlerCalled = true
	}

	r := buildRestRequest(t, "Basic ")
	mw.MiddlewareFunc(handler)(r.responseWriter, r.req)
	if !handlerCalled {
		t.Error("handler should have been called for malformed Basic")
	}
}

func TestBasicAuthToBearerMiddleware_ValidBasic(t *testing.T) {
	originalFactory := BasicAuthTokenFactory
	defer func() { BasicAuthTokenFactory = originalFactory }()

	BasicAuthTokenFactory = func(ctx context.Context, username, password string, jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client, ttl time.Duration) (string, *RError) {
		if username == "alice" && password == "secret" {
			return "mock-jwt", nil
		}
		return "", &RError{Code: http.StatusUnauthorized}
	}

	mw := &BasicAuthToBearerMiddleware{}
	handlerCalled := false
	var gotAuth string
	handler := func(w rest.ResponseWriter, r *rest.Request) {
		handlerCalled = true
		gotAuth = r.Header.Get("Authorization")
	}

	creds := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	r := buildRestRequest(t, "Basic "+creds)
	mw.MiddlewareFunc(handler)(r.responseWriter, r.req)

	if !handlerCalled {
		t.Fatal("handler should have been called")
	}
	if gotAuth != "Bearer mock-jwt" {
		t.Errorf("Authorization header = %q, want \"Bearer mock-jwt\"", gotAuth)
	}
	if r.req.Env["PH_BASIC_AUTH_USER"] != "alice" {
		t.Errorf("PH_BASIC_AUTH_USER = %v, want \"alice\"", r.req.Env["PH_BASIC_AUTH_USER"])
	}
}

func TestBasicAuthToBearerMiddleware_InvalidBasic(t *testing.T) {
	oldPort := os.Getenv(EnvFluentPort)
	os.Setenv(EnvFluentPort, "")
	defer os.Setenv(EnvFluentPort, oldPort)
	originalFactory := BasicAuthTokenFactory
	defer func() { BasicAuthTokenFactory = originalFactory }()

	BasicAuthTokenFactory = func(ctx context.Context, username, password string, jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client, ttl time.Duration) (string, *RError) {
		return "", &RError{Code: http.StatusUnauthorized}
	}

	mw := &BasicAuthToBearerMiddleware{}
	handlerCalled := false
	handler := func(w rest.ResponseWriter, r *rest.Request) {
		handlerCalled = true
	}

	creds := base64.StdEncoding.EncodeToString([]byte("bob:wrong"))
	r := buildRestRequest(t, "Basic "+creds)
	mw.MiddlewareFunc(handler)(r.responseWriter, r.req)

	if handlerCalled {
		t.Error("handler should NOT have been called")
	}
	rec := r.responseWriter.(*testResponseWriter).ResponseRecorder
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("WWW-Authenticate header should be set")
	}
}

func TestBasicAuthToBearerMiddleware_FactoryNil(t *testing.T) {
	BasicAuthTokenFactory = nil
	mw := &BasicAuthToBearerMiddleware{}
	handlerCalled := false
	handler := func(w rest.ResponseWriter, r *rest.Request) {
		handlerCalled = true
	}

	creds := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	r := buildRestRequest(t, "Basic "+creds)
	mw.MiddlewareFunc(handler)(r.responseWriter, r.req)
	if !handlerCalled {
		t.Error("handler should have been called when factory is nil")
	}
}

// testRequest wraps a *rest.Request so we can build it easily.
type testRequest struct {
	req            *rest.Request
	responseWriter rest.ResponseWriter
}

func buildRestRequest(t *testing.T, authz string) *testRequest {
	t.Helper()
	httpReq := httptest.NewRequest("GET", "/", nil)
	if authz != "" {
		httpReq.Header.Set("Authorization", authz)
	}
	req := &rest.Request{Request: httpReq, Env: map[string]interface{}{}}
	recorder := httptest.NewRecorder()
	rw := &testResponseWriter{recorder, false}
	return &testRequest{req: req, responseWriter: rw}
}

// testResponseWriter implements rest.ResponseWriter for tests.
type testResponseWriter struct {
	*httptest.ResponseRecorder
	wroteHeader bool
}

func (w *testResponseWriter) WriteJson(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (w *testResponseWriter) EncodeJson(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (w *testResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.ResponseRecorder.WriteHeader(code)
		w.wroteHeader = true
	}
}

func (w *testResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseRecorder.Write(b)
}

func (w *testResponseWriter) Count() uint64 {
	return 0
}
