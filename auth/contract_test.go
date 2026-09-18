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

package auth

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Contract test for /auth: responses for every route with and without a
// session, plus login/refresh flows. Recorded against go-json-rest; the echo
// port must reproduce it.

var updateContract = flag.Bool("update-contract", false, "rewrite testdata/contract.golden.json")

const (
	contractKey   = "auth-contract-key"
	contractRealm = `"pantahub services", ph-aeps="https://api.example.com/auth"`
)

type contractResult struct {
	Status          int
	ContentType     string
	WWWAuthenticate string
	Location        string
	Cookies         []string
	Body            string
}

var (
	jwtRE          = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	volatileRE     = regexp.MustCompile(`REST-ERR-ID-\d+|"incident":\d+|state=[^&"\\]+|[0-9a-f]{24}|\d{4}-\d{2}-\d{2}T[0-9:.]+(Z|[+-]\d{2}:\d{2})|\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	volatileClaims = map[string]bool{"iat": true, "orig_iat": true, "jti": true, "nbf": true}
	unixFieldRE    = regexp.MustCompile(`"(exp|iat|orig_iat|nbf)":\d{9,}`)
)

// maskToken replaces a JWT with its claims, volatile values elided.
func maskToken(tok string) string {
	parts := strings.Split(tok, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "<jwt:undecodable>"
	}
	claims := map[string]interface{}{}
	if json.Unmarshal(raw, &claims) != nil {
		return "<jwt:badjson>"
	}
	keys := make([]string, 0, len(claims))
	for k := range claims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<jwt")
	for _, k := range keys {
		v := "*"
		if k == "exp" {
			if f, ok := claims[k].(float64); ok {
				v = "+" + time.Until(time.Unix(int64(f), 0)).Round(time.Minute).String()
			}
		} else if !volatileClaims[k] {
			vb, _ := json.Marshal(claims[k])
			v = string(vb)
		}
		b.WriteString(" " + k + "=" + v)
	}
	b.WriteString(">")
	return b.String()
}

func mask(s string) string {
	s = jwtRE.ReplaceAllStringFunc(s, maskToken)
	s = unixFieldRE.ReplaceAllString(s, `"$1":<unix>`)
	return volatileRE.ReplaceAllString(s, "<v>")
}

func TestAuthContract(t *testing.T) {
	t.Setenv(utils.EnvFluentPort, "")
	t.Setenv(utils.EnvPantahubMfaEncKey, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv(utils.EnvPantahubMfaEnabled, "true")
	t.Setenv(utils.EnvPantahubAuth, "https://api.example.com/auth")
	h := newContractHandler(t)

	do := func(method, path, token, body string, hdr map[string]string) (contractResult, *httptest.ResponseRecorder) {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.Header.Set("User-Agent", "contract-test")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var cookies []string
		for _, c := range rec.Result().Cookies() {
			cookies = append(cookies, c.Name)
		}
		return contractResult{
			Status:          rec.Code,
			ContentType:     rec.Header().Get("Content-Type"),
			WWWAuthenticate: rec.Header().Get("WWW-Authenticate"),
			Location:        mask(rec.Header().Get("Location")),
			Cookies:         cookies,
			Body:            mask(rec.Body.String()),
		}, rec
	}

	got := map[string]contractResult{}
	record := func(key string, r contractResult) { got[key] = r }

	// Flows first, to obtain tokens.
	r, rec := do("POST", "/auth/login", "", `{"username":"user1","password":"user1"}`, nil)
	record("flow login user1", r)
	var login struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	user := login.Token
	if user == "" {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	r, _ = do("POST", "/auth/login", "", `{"username":"user1","password":"wrong"}`, nil)
	record("flow login bad password", r)
	r, _ = do("POST", "/auth/login", "", `{}`, nil)
	record("flow login empty creds", r)
	r, _ = do("POST", "/auth/login", "", ``, nil)
	record("flow login no body", r)
	r, _ = do("POST", "/auth/login", "", `{"username":"prn:pantahub.com:auth:/anon","password":"x"}`, nil)
	record("flow login anon", r)
	r, _ = do("GET", "/auth/login", user, "", nil)
	record("flow refresh with token", r)
	r, _ = do("GET", "/auth/login", "", "", nil)
	record("flow refresh without token", r)
	r, _ = do("GET", "/auth/login", "garbage", "", nil)
	record("flow refresh garbage token", r)
	noIat, _ := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, jwtgo.MapClaims{
		"id": "user1", "prn": "prn:pantahub.com:auth:/user1", "type": "USER", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(contractKey))
	r, _ = do("GET", "/auth/login", noIat, "", nil)
	record("flow refresh token without orig_iat", r)
	r, _ = do("GET", "/auth/auth_status", user, "", nil)
	record("flow auth_status", r)
	r, _ = do("GET", "/auth/", user, "", nil)
	record("flow profile", r)
	r, _ = do("POST", "/auth/token", user, `{"scopes":"prn:pantahub.com:apis:/base/all"}`, nil)
	record("flow token", r)
	r, _ = do("OPTIONS", "/auth/login", "", "", map[string]string{"Origin": "https://ui.example.com", "Access-Control-Request-Method": "POST"})
	record("flow preflight login", r)
	r, _ = do("GET", "/auth/login", "Basic dXNlcjE6dXNlcjE=", "", nil)
	record("flow refresh basic", r)

	routes := [][2]string{
		{"GET", "/"}, {"POST", "/login"}, {"POST", "/login/mfa/totp"}, {"POST", "/login/mfa/recovery"},
		{"POST", "/login/mfa/webauthn"}, {"POST", "/login/mfa/webauthn/finish"}, {"POST", "/login/webauthn/begin"},
		{"POST", "/login/webauthn/finish"}, {"GET", "/mfa"}, {"POST", "/mfa/totp"}, {"POST", "/mfa/totp/confirm"},
		{"DELETE", "/mfa/totp"}, {"POST", "/mfa/recovery/regenerate"}, {"POST", "/mfa/reauth/totp"},
		{"POST", "/mfa/reauth/recovery"}, {"POST", "/mfa/reauth/webauthn"}, {"POST", "/mfa/reauth/webauthn/finish"},
		{"POST", "/mfa/webauthn/register"}, {"POST", "/mfa/webauthn/register/finish"},
		{"PATCH", "/mfa/webauthn/credentials/c1"}, {"DELETE", "/mfa/webauthn/credentials/c1"},
		{"GET", "/connected-providers"}, {"POST", "/connected-providers"}, {"DELETE", "/connected-providers"},
		{"POST", "/token"}, {"POST", "/token/refresh"}, {"GET", "/auth_status"}, {"GET", "/login"},
		{"GET", "/accounts"}, {"POST", "/accounts"}, {"POST", "/sessions"}, {"GET", "/verify"},
		{"POST", "/recover"}, {"POST", "/password"}, {"POST", "/authorize"}, {"POST", "/code"},
		{"POST", "/signature/verify"}, {"POST", "/x509/login"}, {"GET", "/oauth/login/github"},
		{"GET", "/oauth/callback/github"}, {"POST", "/oauth/token"}, {"GET", "/oauth/authorize"},
		{"POST", "/oauth/authorize"}, {"POST", "/oauth/pkce/init"},
		// unrouted / overlapping
		{"GET", "/nope"}, {"PUT", "/login"}, {"GET", "/mfa/totp"}, {"GET", "/oauth/login"}, {"GET", "/oauth/login/a/b"},
		{"DELETE", "/login/mfa/totp"}, {"GET", "/login/webauthn/x"}, {"POST", "/oauth/tokenx"}, {"GET", "/oauth/authorizex"},
		{"GET", "/login/"}, {"POST", "/accounts/"},
	}
	for _, rt := range routes {
		for _, tok := range []struct{ name, v string }{{"none", ""}, {"user", user}, {"bad", "garbage"}} {
			r, _ := do(rt[0], "/auth"+rt[1], tok.v, "", nil)
			record(rt[0]+" "+rt[1]+" token="+tok.name, r)
			r, _ = do(rt[0], "/auth"+rt[1], tok.v, "{}", nil)
			record(rt[0]+" "+rt[1]+" token="+tok.name+" body={}", r)
		}
	}

	const golden = "testdata/contract.golden.json"
	b, _ := json.MarshalIndent(got, "", "  ")
	if *updateContract {
		if err := os.WriteFile(golden, append(b, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	var wantMap map[string]contractResult
	if err := json.Unmarshal(want, &wantMap); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(wantMap))
	for k := range wantMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s: missing", k)
			continue
		}
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(wantMap[k])
		if string(gb) != string(wb) {
			t.Errorf("%s:\n got  %s\n want %s", k, gb, wb)
		}
	}
	if len(got) != len(wantMap) {
		t.Errorf("got %d cases, golden has %d", len(got), len(wantMap))
	}
}
