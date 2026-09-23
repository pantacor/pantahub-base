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

	jwtgo "github.com/golang-jwt/jwt/v5"
)

// Tokens minted by dgrijalva/jwt-go v3.2.0 before the v5 move. Do not regenerate:
// they prove old device tokens still verify. HS256, exp in 2123.
const (
	fixtureKey = "fixture-hmac-key-do-not-use-in-prod"

	// MapClaims, shaped like what LoginHandler mints.
	v3MapClaimsToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJwcm46cGFudGFodWIuY29tOmFwaXM6L2FwaSIsImV4cCI6NDg1MzYwMDAwMCwiaWQiOiJhZG1pbiIsIm5pY2siOiJhZG1pbiIsIm9yaWdfaWF0IjoxNzAwMDAwMDAwfQ.byLxgggxEL7Y_Fff1_0LjRrRvNBPEG3b1jf4RjpW5FQ"

	// Typed claims with an embedded StandardClaims (v5: RegisteredClaims).
	v3StructClaimsToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImFkbWluIiwicHJuIjoicHJuOjo6YWNjb3VudHM6L2FkbWluIiwic2NvcGUiOiJhbGwiLCJtZXRob2RzIjpbInBhc3N3b3JkIl0sImF1ZCI6InBybjpwYW50YWh1Yi5jb206YXBpczovYXBpIiwiZXhwIjo0ODUzNjAwMDAwLCJqdGkiOiIwYmFkYzBmZmVlIiwiaWF0IjoxNzAwMDAwMDAwLCJzdWIiOiJwcm46OjphY2NvdW50czovYWRtaW4ifQ.hCDS57g6gmuaL2jOf71tKKbzwC0keQKUXdivPw98k7s"
)

func keyFunc(*jwtgo.Token) (interface{}, error) { return []byte(fixtureKey), nil }

// A v3-minted MapClaims token must still parse, verify and validate.
func TestV3MapClaimsTokenStillVerifies(t *testing.T) {
	tok, err := jwtgo.Parse(v3MapClaimsToken, keyFunc)
	if err != nil {
		t.Fatalf("parsing a v3-minted token: %v", err)
	}
	if !tok.Valid {
		t.Fatal("token did not validate")
	}

	claims, ok := tok.Claims.(jwtgo.MapClaims)
	if !ok {
		t.Fatalf("claims are %T, want MapClaims", tok.Claims)
	}
	for k, want := range map[string]interface{}{
		"id":   "admin",
		"nick": "admin",
		"aud":  "prn:pantahub.com:apis:/api",
	} {
		if got := claims[k]; got != want {
			t.Errorf("claim %q = %v, want %v", k, got, want)
		}
	}
	// orig_iat drives RefreshHandler's "too old to refresh" arithmetic, and it
	// must keep decoding as a JSON number.
	if _, ok := claims["orig_iat"].(float64); !ok {
		t.Errorf("orig_iat is %T, want float64", claims["orig_iat"])
	}
}

// The production verifier (ParseAuthorizationHeader) must accept the same token.
func TestV3MapClaimsTokenVerifiesThroughConfig(t *testing.T) {
	c := &Config{Realm: "r", Key: []byte(fixtureKey)}
	c.ApplyDefaults()
	tok, err := c.ParseAuthorizationHeader("Bearer " + v3MapClaimsToken)
	if err != nil || !tok.Valid {
		t.Fatalf("v3 token rejected by Config: %v", err)
	}
}

// Parsed as MapClaims to check the token bytes, independent of the claims type.
func TestV3StructClaimsTokenStillVerifies(t *testing.T) {
	tok, err := jwtgo.Parse(v3StructClaimsToken, keyFunc)
	if err != nil {
		t.Fatalf("parsing a v3-minted struct-claims token: %v", err)
	}
	if !tok.Valid {
		t.Fatal("token did not validate")
	}

	claims := tok.Claims.(jwtgo.MapClaims)
	for k, want := range map[string]interface{}{
		"username": "admin",
		"prn":      "prn:::accounts:/admin",
		"scope":    "all",
		"jti":      "0badc0ffee",
		"sub":      "prn:::accounts:/admin",
		// v3 emitted a single audience as a bare string, not an array.
		"aud": "prn:pantahub.com:apis:/api",
	} {
		if got := claims[k]; got != want {
			t.Errorf("claim %q = %v, want %v", k, got, want)
		}
	}
}

// A token signed with the wrong key must be rejected. Guards against a keyfunc
// or validation regression that would accept anything.
func TestWrongKeyIsRejected(t *testing.T) {
	_, err := jwtgo.Parse(v3MapClaimsToken, func(*jwtgo.Token) (interface{}, error) {
		return []byte("a-completely-different-key"), nil
	})
	if err == nil {
		t.Fatal("a token signed with a different key was accepted")
	}
}

// A single audience must encode as a string, not an array (see init).
func TestSingleAudienceMarshalsAsBareString(t *testing.T) {
	tok := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, jwtgo.RegisteredClaims{
		Audience: jwtgo.ClaimStrings{"prn:pantahub.com:apis:/api"},
	})
	signed, err := tok.SignedString([]byte(fixtureKey))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(signed, ".")[1])
	if err != nil {
		t.Fatalf("decoding payload: %v", err)
	}

	const want = `"aud":"prn:pantahub.com:apis:/api"`
	if !strings.Contains(string(payload), want) {
		t.Errorf("payload = %s\nwant it to contain %s\n"+
			"(if this shows [\"...\"] then MarshalSingleStringAsArray got re-enabled)",
			payload, want)
	}
}
