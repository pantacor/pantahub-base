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

package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Contract test for the proxy: what callers get back and exactly what the
// upstream receives, with the v2 signature verified. The golden file was
// recorded against the go-json-rest implementation.

var update = flag.Bool("update", false, "rewrite testdata/proxy_contract.golden.json")

const (
	testKey    = "webhooks-contract-key"
	testRealm  = `"pantahub services", ph-aeps="https://api.example.com/auth"`
	testSecret = "proxy-secret"
)

func TestMain(m *testing.M) {
	os.Setenv("FLUENT_PORT", "")
	os.Exit(m.Run())
}

type upstreamSeen struct {
	Method, Path, RawQuery, Body             string
	Caller, Owner, Type, Scopes              string
	HasAuthorization, HasCookie, SignatureOK bool
}

type contractResult struct {
	Status          int
	ContentType     string
	WWWAuthenticate string
	Body            string
	Upstream        *upstreamSeen
}

var volatile = regexp.MustCompile(`"incident":\d+|REST-ERR-ID-\d+`)

func TestProxyContract(t *testing.T) {
	utils.InitScopes()
	var seen *upstreamSeen
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(body)
		base := strings.Join([]string{
			"v2", r.Header.Get("X-Pantahub-Proxy-Timestamp"), r.Header.Get("X-Pantahub-Proxy-Nonce"),
			r.Method, r.URL.Path, canonicalQuery(r.URL.RawQuery), hex.EncodeToString(sum[:]),
			r.Header.Get("X-Pantahub-Owner"), r.Header.Get("X-Pantahub-Caller"),
			r.Header.Get("X-Pantahub-Type"), r.Header.Get("X-Pantahub-Scopes"),
		}, "\n")
		mac := hmac.New(sha256.New, []byte(testSecret))
		mac.Write([]byte(base))
		seen = &upstreamSeen{
			Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Body: string(body),
			Caller: r.Header.Get("X-Pantahub-Caller"), Owner: r.Header.Get("X-Pantahub-Owner"),
			Type: r.Header.Get("X-Pantahub-Type"), Scopes: r.Header.Get("X-Pantahub-Scopes"),
			HasAuthorization: r.Header.Get("Authorization") != "",
			HasCookie:        r.Header.Get("Cookie") != "",
			SignatureOK:      r.Header.Get("X-Pantahub-Proxy-Signature") == "v2="+hex.EncodeToString(mac.Sum(nil)),
		}
		w.Header().Set("Server", "upstream-secret")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"upstream":true}`))
	}))
	defer backend.Close()
	t.Setenv("PANTAHUB_WEBHOOKS_BACKEND", backend.URL)
	t.Setenv("PANTAHUB_WEBHOOKS_PROXY_SECRET", testSecret)

	h := newHandler(t)

	token := func(scopes string) string {
		claims := jwtgo.MapClaims{
			"id": "user1", "nick": "user1", "prn": "prn:::accounts:/u1", "owner": "prn:::accounts:/u1",
			"type": "USER", "exp": time.Now().Add(time.Hour).Unix(),
		}
		if scopes != "" {
			claims["scopes"] = scopes
		}
		s, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claims).SignedString([]byte(testKey))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	all := token(strings.Join(utils.MarshalScopes([]utils.Scope{utils.Scopes.API}), " "))
	readOnly := token(strings.Join(utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadWebhooks}), " "))
	noScopes := token("")

	type req struct{ Method, Path, Token, Body string }
	var reqs []req
	for _, tok := range []struct{ name, v string }{{"none", ""}, {"all", all}, {"readonly", readOnly}, {"noscopes", noScopes}} {
		for _, mp := range [][2]string{
			{"GET", "/webhooks/event-types"}, {"GET", "/webhooks/event-types/x"}, {"POST", "/webhooks/event-types"},
			{"GET", "/webhooks/"}, {"POST", "/webhooks/"}, {"GET", "/webhooks/events"}, {"GET", "/webhooks/events/e1"},
			{"GET", "/webhooks/events/e1/deliveries"}, {"POST", "/webhooks/events/e1/redeliver"},
			{"GET", "/webhooks/h1"}, {"PUT", "/webhooks/h1"}, {"PATCH", "/webhooks/h1"}, {"DELETE", "/webhooks/h1"},
			{"POST", "/webhooks/h1/rotate-secret"}, {"POST", "/webhooks/h1/test"}, {"GET", "/webhooks/h1/deliveries"},
			{"GET", "/webhooks/h1/deliveries/d1"}, {"POST", "/webhooks/h1/deliveries/d1/replay"},
			{"GET", "/webhooks/events/deliveries"}, {"GET", "/webhooks/h1/deliveries/d1/replay"},
			{"GET", "/webhooks/a/b/c/d"}, {"GET", "/webhooks/h1/"}, {"POST", "/webhooks/h1"},
			{"GET", "/webhooks/?b=2&a=1&a=0"}, {"GET", "/webhooks/h%2F1"},
		} {
			reqs = append(reqs, req{mp[0], mp[1], tok.v, ""})
		}
		reqs = append(reqs, req{"PUT", "/webhooks/h1", tok.v, `{"url":"https://x"}`})
	}
	names := map[string]string{"": "none", all: "all", readOnly: "readonly", noScopes: "noscopes"}

	got := map[string]contractResult{}
	for _, rq := range reqs {
		seen = nil
		var body io.Reader
		if rq.Body != "" {
			body = strings.NewReader(rq.Body)
		}
		r := httptest.NewRequest(rq.Method, rq.Path, body)
		if rq.Token != "" {
			r.Header.Set("Authorization", "Bearer "+rq.Token)
		}
		r.Header.Set("Cookie", "session=1")
		r.Header.Set("X-Pantahub-Caller", "prn:::accounts:/spoofed")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		key := rq.Method + " " + rq.Path + " token=" + names[rq.Token]
		if rq.Body != "" {
			key += " body"
		}
		got[key] = contractResult{
			Status:          rec.Code,
			ContentType:     rec.Header().Get("Content-Type"),
			WWWAuthenticate: rec.Header().Get("WWW-Authenticate"),
			Body:            volatile.ReplaceAllString(rec.Body.String(), "<volatile>"),
			Upstream:        seen,
		}
		if rec.Header().Get("Server") != "" {
			t.Errorf("%s: upstream Server header leaked", key)
		}
	}

	const golden = "testdata/proxy_contract.golden.json"
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if *update {
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
	for k, w := range wantMap {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s: missing", k)
			continue
		}
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		if string(gb) != string(wb) {
			t.Errorf("%s:\n got  %s\n want %s", k, gb, wb)
		}
	}
	if len(got) != len(wantMap) {
		t.Errorf("got %d cases, golden has %d", len(got), len(wantMap))
	}
}
