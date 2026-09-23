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

package jwtauth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const key = "test-key"

func cfg() *Config {
	c := &Config{Realm: `"pantahub services", ph-aeps="https://api.example.com/auth"`, Key: []byte(key)}
	c.ApplyDefaults()
	return c
}

func sign(t *testing.T, method jwt.SigningMethod, k interface{}, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(k)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseAuthorizationHeader(t *testing.T) {
	c := cfg()
	valid := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"id": "u1", "exp": time.Now().Add(time.Hour).Unix()})
	expired := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()})
	wrongKey := sign(t, jwt.SigningMethodHS256, []byte("other"), jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()})
	wrongAlg := sign(t, jwt.SigningMethodHS512, []byte(key), jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()})

	for _, tc := range []struct {
		name, header string
		ok           bool
	}{
		{"valid", "Bearer " + valid, true},
		{"empty", "", false},
		{"lowercase scheme", "bearer " + valid, false},
		{"no scheme", valid, false},
		{"expired", "Bearer " + expired, false},
		{"wrong key", "Bearer " + wrongKey, false},
		{"wrong algorithm", "Bearer " + wrongAlg, false},
		{"garbage", "Bearer x.y.z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := c.ParseAuthorizationHeader(tc.header)
			if tc.ok && (err != nil || tok == nil || !tok.Valid) {
				t.Fatalf("want valid, got err=%v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("want rejection, got none")
			}
		})
	}
}

func TestWWWAuthenticateIsExact(t *testing.T) {
	const want = `JWT realm="pantahub services", ph-aeps="https://api.example.com/auth"`
	if got := cfg().WWWAuthenticate(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A single audience must encode as a string, not an array.
func TestSingleAudienceIsString(t *testing.T) {
	s := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"aud": jwt.ClaimStrings{"prn:x"}})
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(s, ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"aud":"prn:x"`) {
		t.Fatalf("payload %s: aud not a string", payload)
	}
}

func TestRefresh(t *testing.T) {
	c := cfg()
	c.MaxRefresh = 24 * time.Hour
	now := time.Now()
	fresh := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"id": "u1", "prn": "p", "exp": now.Add(time.Hour).Unix(), "orig_iat": now.Unix()})
	old := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"id": "u1", "exp": now.Add(time.Hour).Unix(), "orig_iat": now.Add(-48 * time.Hour).Unix()})
	noIat := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"id": "u1", "exp": now.Add(time.Hour).Unix()})
	strIat := sign(t, jwt.SigningMethodHS256, []byte(key), jwt.MapClaims{"id": "u1", "exp": now.Add(time.Hour).Unix(), "orig_iat": "x"})

	out, err := c.Refresh("Bearer "+fresh, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := c.ParseAuthorizationHeader("Bearer " + out)
	if err != nil {
		t.Fatal(err)
	}
	claims := tok.Claims.(jwt.MapClaims)
	if claims["prn"] != "p" || int64(claims["orig_iat"].(float64)) != now.Unix() {
		t.Fatalf("claims not carried over: %v", claims)
	}
	if exp := int64(claims["exp"].(float64)); exp < now.Add(2*time.Hour-time.Minute).Unix() || exp > now.Add(2*time.Hour+time.Minute).Unix() {
		t.Fatalf("exp %d not now+2h", exp)
	}

	for _, tc := range []struct {
		name, header string
		want         error
	}{
		{"too old", "Bearer " + old, ErrTooOld},
		{"no orig_iat", "Bearer " + noIat, ErrNotRefreshable},
		{"non-numeric orig_iat", "Bearer " + strIat, ErrNotRefreshable},
	} {
		if _, err := c.Refresh(tc.header, time.Hour); err != tc.want {
			t.Errorf("%s: err=%v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := c.Refresh("", time.Hour); err == nil {
		t.Error("missing header: want error")
	}
	if _, err := c.Refresh("Bearer garbage", time.Hour); err == nil {
		t.Error("garbage token: want error")
	}
}
