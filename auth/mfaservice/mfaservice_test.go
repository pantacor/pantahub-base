// Copyright 2026  Pantacor Ltd.
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

package mfaservice

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func setEncKey(t *testing.T) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(utils.EnvPantahubMfaEncKey, base64.StdEncoding.EncodeToString(key))
}

func testMiddleware() *jwt.JWTMiddleware {
	return &jwt.JWTMiddleware{
		Key:              []byte("test secret key"),
		Realm:            "pantahub services",
		SigningAlgorithm: "HS256",
		Timeout:          time.Minute * 60,
	}
}

func generateCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period:    TOTPPeriod,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestGenerateTOTPKey(t *testing.T) {
	key, err := GenerateTOTPKey("Pantacor Hub", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if key.Secret() == "" {
		t.Error("expected a non-empty secret")
	}

	url := key.URL()
	for _, want := range []string{
		"otpauth://totp/", "algorithm=SHA1", "digits=6", "period=30",
		"issuer=Pantacor", "user@example.com",
	} {
		if !strings.Contains(url, want) {
			t.Errorf("otpauth url %q misses %q", url, want)
		}
	}
}

func TestVerifyTOTPCode(t *testing.T) {
	key, err := GenerateTOTPKey("Pantacor Hub", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	secret := key.Secret()
	now := time.Unix(1754500000, 0) // fixed, mid-step

	t.Run("current step matches", func(t *testing.T) {
		code := generateCode(t, secret, now)
		step, ok := VerifyTOTPCode(secret, code, now)
		if !ok {
			t.Fatal("expected current-step code to verify")
		}
		if step != now.Unix()/TOTPPeriod {
			t.Errorf("expected step %d got %d", now.Unix()/TOTPPeriod, step)
		}
	})

	t.Run("previous step within skew", func(t *testing.T) {
		code := generateCode(t, secret, now.Add(-TOTPPeriod*time.Second))
		step, ok := VerifyTOTPCode(secret, code, now)
		if !ok {
			t.Fatal("expected -1 step code to verify")
		}
		if step != now.Unix()/TOTPPeriod-1 {
			t.Errorf("expected step %d got %d", now.Unix()/TOTPPeriod-1, step)
		}
	})

	t.Run("next step within skew", func(t *testing.T) {
		code := generateCode(t, secret, now.Add(TOTPPeriod*time.Second))
		step, ok := VerifyTOTPCode(secret, code, now)
		if !ok {
			t.Fatal("expected +1 step code to verify")
		}
		if step != now.Unix()/TOTPPeriod+1 {
			t.Errorf("expected step %d got %d", now.Unix()/TOTPPeriod+1, step)
		}
	})

	t.Run("outside skew rejected", func(t *testing.T) {
		code := generateCode(t, secret, now.Add(-2*TOTPPeriod*time.Second))
		if _, ok := VerifyTOTPCode(secret, code, now); ok {
			t.Error("expected -2 step code to be rejected")
		}
	})

	t.Run("garbage rejected", func(t *testing.T) {
		for _, bad := range []string{"", "12345", "1234567", "abcdef", "000000"} {
			if _, ok := VerifyTOTPCode(secret, bad, now); ok {
				// 000000 could in theory be the real code; regenerate check
				if bad == "000000" && generateCode(t, secret, now) == "000000" {
					continue
				}
				t.Errorf("expected code %q to be rejected", bad)
			}
		}
	})

	t.Run("whitespace tolerated", func(t *testing.T) {
		code := generateCode(t, secret, now)
		if _, ok := VerifyTOTPCode(secret, "  "+code+" ", now); !ok {
			t.Error("expected code with surrounding whitespace to verify")
		}
	})
}

func TestEncryptDecryptSecret(t *testing.T) {
	setEncKey(t)

	const plain = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	enc, err := EncryptSecret(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), plain) {
		t.Fatal("ciphertext contains the plaintext secret")
	}

	dec, err := DecryptSecret(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != plain {
		t.Errorf("round trip mismatch: %q != %q", dec, plain)
	}

	t.Run("unique nonces", func(t *testing.T) {
		enc2, err := EncryptSecret(plain)
		if err != nil {
			t.Fatal(err)
		}
		if string(enc) == string(enc2) {
			t.Error("two encryptions of the same secret must differ")
		}
	})

	t.Run("wrong key fails", func(t *testing.T) {
		setEncKey(t) // fresh key
		if _, err := DecryptSecret(enc); err == nil {
			t.Error("expected decryption with a different key to fail")
		}
	})

	t.Run("missing key is ErrMFANotConfigured", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubMfaEncKey, "")
		if _, err := EncryptSecret(plain); err != ErrMFANotConfigured {
			t.Errorf("expected ErrMFANotConfigured, got %v", err)
		}
	})

	t.Run("bad key length is ErrMFANotConfigured", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubMfaEncKey, base64.StdEncoding.EncodeToString([]byte("short")))
		if _, err := EncryptSecret(plain); err != ErrMFANotConfigured {
			t.Errorf("expected ErrMFANotConfigured, got %v", err)
		}
	})
}

func TestRecoveryCodes(t *testing.T) {
	plain, hashed, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}

	if len(plain) != RecoveryCodeCount || len(hashed) != RecoveryCodeCount {
		t.Fatalf("expected %d codes, got %d/%d", RecoveryCodeCount, len(plain), len(hashed))
	}

	seen := map[string]bool{}
	for _, code := range plain {
		if len(code) != 11 || code[5] != '-' {
			t.Errorf("unexpected code format: %q", code)
		}
		if seen[code] {
			t.Errorf("duplicate recovery code generated: %q", code)
		}
		seen[code] = true
	}

	t.Run("valid code found", func(t *testing.T) {
		index, ok := VerifyRecoveryCode(hashed, plain[3])
		if !ok || index != 3 {
			t.Errorf("expected (3, true), got (%d, %v)", index, ok)
		}
	})

	t.Run("normalization", func(t *testing.T) {
		messy := "  " + strings.ToUpper(plain[0]) + " "
		index, ok := VerifyRecoveryCode(hashed, messy)
		if !ok || index != 0 {
			t.Errorf("expected (0, true) for %q, got (%d, %v)", messy, index, ok)
		}
	})

	t.Run("used code rejected", func(t *testing.T) {
		now := time.Now()
		hashed[3].UsedAt = &now
		if _, ok := VerifyRecoveryCode(hashed, plain[3]); ok {
			t.Error("expected used code to be rejected")
		}
	})

	t.Run("unknown code rejected", func(t *testing.T) {
		if _, ok := VerifyRecoveryCode(hashed, "nope1-nope2"); ok {
			t.Error("expected unknown code to be rejected")
		}
	})
}

func TestMFAPendingToken(t *testing.T) {
	mw := testMiddleware()

	mint := func(t *testing.T) (string, *MFAPendingClaims) {
		t.Helper()
		token, err := CreateMFAPendingToken(mw, "user1", "prn:pantahub.com:auth:/user1", "", []string{"pwd"}, []string{"totp", "recovery"})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := ParseMFAPendingToken(mw, token)
		if err != nil {
			t.Fatal(err)
		}
		return token, claims
	}

	t.Run("round trip", func(t *testing.T) {
		_, claims := mint(t)
		if claims.Username != "user1" ||
			claims.Prn != "prn:pantahub.com:auth:/user1" ||
			claims.TokenUse != MFAPendingTokenUse ||
			claims.Id == "" {
			t.Errorf("unexpected claims: %+v", claims)
		}
		if !claims.HasMethod("totp") || !claims.HasMethod("recovery") || claims.HasMethod("webauthn") {
			t.Errorf("unexpected methods: %v", claims.Methods)
		}
		ttl := time.Until(time.Unix(claims.ExpiresAt, 0))
		if ttl <= 0 || ttl > PendingTokenTimeout() {
			t.Errorf("unexpected ttl: %s", ttl)
		}
	})

	t.Run("unique jti", func(t *testing.T) {
		_, c1 := mint(t)
		_, c2 := mint(t)
		if c1.Id == c2.Id {
			t.Error("jti must be unique per token")
		}
	})

	t.Run("tampered rejected", func(t *testing.T) {
		token, _ := mint(t)
		if _, err := ParseMFAPendingToken(mw, token[:len(token)-2]); err == nil {
			t.Error("expected tampered token to be rejected")
		}
	})

	t.Run("wrong key rejected", func(t *testing.T) {
		token, _ := mint(t)
		otherMw := testMiddleware()
		otherMw.Key = []byte("a different key")
		if _, err := ParseMFAPendingToken(otherMw, token); err == nil {
			t.Error("expected token signed with another key to be rejected")
		}
	})

	t.Run("expired rejected", func(t *testing.T) {
		claims := &MFAPendingClaims{
			TokenUse: MFAPendingTokenUse,
			Prn:      "prn:pantahub.com:auth:/user1",
			StandardClaims: jwtgo.StandardClaims{
				Id:        "deadbeef",
				IssuedAt:  time.Now().Add(-10 * time.Minute).Unix(),
				ExpiresAt: time.Now().Add(-5 * time.Minute).Unix(),
			},
		}
		token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claims)
		signed, err := token.SignedString(mw.Key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseMFAPendingToken(mw, signed); err == nil {
			t.Error("expected expired token to be rejected")
		}
	})

	t.Run("sudo token round trip, and cross-rejection with pending", func(t *testing.T) {
		sudo, err := CreateSudoToken(mw, "prn:pantahub.com:auth:/user1", "otp")
		if err != nil {
			t.Fatal(err)
		}
		claims, err := ParseSudoToken(mw, sudo)
		if err != nil {
			t.Fatalf("sudo token must parse: %v", err)
		}
		if claims.Prn != "prn:pantahub.com:auth:/user1" {
			t.Errorf("unexpected sudo prn: %s", claims.Prn)
		}
		// a sudo token must not pass as a pending token and vice versa
		if _, err := ParseMFAPendingToken(mw, sudo); err == nil {
			t.Error("sudo token must be rejected as a pending token")
		}
		pending, _ := CreateMFAPendingToken(mw, "user1", "prn:pantahub.com:auth:/user1", "", []string{"pwd"}, []string{"totp"})
		if _, err := ParseSudoToken(mw, pending); err == nil {
			t.Error("pending token must be rejected as a sudo token")
		}
	})

	t.Run("session token rejected as pending token", func(t *testing.T) {
		// a real session token (with prn/type claims but no token_use) must
		// not be accepted by the step-up endpoints
		token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, jwtgo.MapClaims{
			"prn":  "prn:pantahub.com:auth:/user1",
			"type": "USER",
			"exp":  time.Now().Add(time.Hour).Unix(),
		})
		signed, err := token.SignedString(mw.Key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseMFAPendingToken(mw, signed); err == nil {
			t.Error("expected session token to be rejected as pending token")
		}
	})
}
