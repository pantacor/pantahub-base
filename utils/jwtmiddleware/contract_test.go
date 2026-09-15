package jwtmiddleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
)

// These tests pin the observable behaviour that clients depend on, using
// net/http/httptest directly rather than go-json-rest's rest/test harness.
//
// That choice is deliberate. The vendored upstream TestAuthJWT fails against the
// Pantacor go-json-rest fork -- it reports "401 expected, got 200" -- and it
// fails identically when run against the pristine upstream module, so it is not
// a vendoring regression. Driving the same middleware through httptest shows the
// middleware behaves correctly (401, handler not reached), which locates the
// fault in the rest/test harness rather than in the code under test. See
// TestUpstreamAuthJWTHarness in auth_jwt_test.go, which is skipped for this
// reason.
//
// The assertions below are the ones the go-json-rest -> echo migration must not
// break. They are written against behaviour observed on stage, not against what
// the implementation happens to do.

func newTestMW() *JWTMiddleware {
	return &JWTMiddleware{
		Realm:         `"pantahub services", ph-aeps="https://api.example.com/auth"`,
		Key:           []byte("secret key"),
		Timeout:       time.Hour,
		MaxRefresh:    time.Hour * 24,
		Authenticator: func(userID, password string) bool { return userID == "admin" && password == "admin" },
	}
}

func serve(t *testing.T, mw *JWTMiddleware, method, authHeader string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	reached := false
	api := rest.NewApi()
	api.Use(mw)
	api.SetApp(rest.AppSimple(func(w rest.ResponseWriter, r *rest.Request) {
		reached = true
		if err := w.WriteJson(map[string]string{"ok": "yes"}); err != nil {
			t.Fatalf("WriteJson: %v", err)
		}
	}))

	req := httptest.NewRequest(method, "http://localhost/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	api.MakeHandler().ServeHTTP(rec, req)
	return rec, reached
}

// The WWW-Authenticate header is not decoration. pvr keys its on-disk
// credential store by a string built from it -- ph-aeps plus " realm=" plus the
// dequoted realm -- so a change to these bytes makes every pvr client and every
// device fail to find its stored token. echo's echojwt emits no such header by
// default, which is why this test exists.
func TestUnauthorizedEmitsWWWAuthenticateVerbatim(t *testing.T) {
	mw := newTestMW()
	rec, reached := serve(t, mw, "GET", "")

	if reached {
		t.Fatal("handler was reached without credentials")
	}
	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	want := "JWT realm=" + mw.Realm
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate =\n  %q\nwant\n  %q", got, want)
	}
}

// Two error shapes exist in this codebase. This middleware emits the
// go-json-rest one, {"Error":...} with a capital E. echo's
// DefaultHTTPErrorHandler emits {"message":...}, which would break every client
// that parses this.
func TestUnauthorizedBodyShape(t *testing.T) {
	rec, _ := serve(t, newTestMW(), "GET", "")

	const want = `{"Error":"Not Authorized"}`
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	// Byte-exact, including the charset parameter: this is what stage returns
	// (verified against api.stage.pantahub.com) and clients match on it.
	const wantCT = "application/json; charset=utf-8"
	if ct := rec.Header().Get("Content-Type"); ct != wantCT {
		t.Errorf("Content-Type = %q, want %q", ct, wantCT)
	}
}

func TestMalformedAuthorizationHeaders(t *testing.T) {
	// "bearer" lower-case is rejected: parseToken compares the scheme with ==,
	// not case-insensitively. That is stricter than RFC 7235 but it is the
	// shipped behaviour, and loosening it during the port would be a silent
	// change in who can authenticate.
	for _, tc := range []struct{ name, header string }{
		{"empty", ""},
		{"lowercase bearer", "bearer sometoken"},
		{"no scheme", "sometoken"},
		{"wrong scheme", "Basic sometoken"},
		{"scheme only", "Bearer"},
		{"garbage token", "Bearer not-a-jwt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, reached := serve(t, newTestMW(), "GET", tc.header)
			if reached {
				t.Error("handler reached with invalid credentials")
			}
			if rec.Code != 401 {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// RefreshHandler asserts originalClaims["orig_iat"].(float64) unchecked, so a
// token without orig_iat panics. auth/service.go wraps it in a recover for
// exactly this reason. Pinning it here means the port cannot quietly change
// which tokens are refreshable, and documents why that wrapper exists.
func TestRefreshHandlerPanicsWithoutOrigIat(t *testing.T) {
	mw := newTestMW()
	mw.MaxRefresh = 0 // LoginHandler then omits orig_iat entirely

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected a panic from the unchecked orig_iat assertion; " +
				"if this no longer panics, auth/service.go's safeRefreshHandler " +
				"wrapper can be revisited")
		}
	}()

	api := rest.NewApi()
	api.SetApp(rest.AppSimple(mw.RefreshHandler))
	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.Header.Set("Authorization", "Bearer "+makeTokenString("admin", []byte("secret key")))
	api.MakeHandler().ServeHTTP(httptest.NewRecorder(), req)
}
