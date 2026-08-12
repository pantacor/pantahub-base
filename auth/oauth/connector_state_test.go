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

package oauth

import (
	"testing"
	"time"
)

// The connect flow binds the authenticated account PRN into the signed OAuth
// state so the callback needs no cross-site cookie. The signature must make the
// PRN unforgeable, and stale states must be rejected.
func TestOAuthStateCarriesConnectPRN(t *testing.T) {
	state, err := encodeState(stateClaims{
		Nonce:       "nonce",
		RedirectURI: "https://hub.pantacor.com/settings/security",
		ConnectPRN:  "prn:::accounts:/user",
		IssuedAt:    time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("encodeState() error = %v", err)
	}

	claims, err := decodeState(state)
	if err != nil {
		t.Fatalf("decodeState() error = %v", err)
	}
	if claims.ConnectPRN != "prn:::accounts:/user" {
		t.Fatalf("ConnectPRN = %q", claims.ConnectPRN)
	}
	if claims.RedirectURI != "https://hub.pantacor.com/settings/security" {
		t.Fatalf("RedirectURI = %q", claims.RedirectURI)
	}

	if _, err := decodeState(state + "A"); err == nil {
		t.Fatal("tampered state was accepted")
	}

	expired, err := encodeState(stateClaims{
		Nonce:      "nonce",
		ConnectPRN: "prn:::accounts:/user",
		IssuedAt:   time.Now().Add(-stateTTL - time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("encodeState() error = %v", err)
	}
	if _, err := decodeState(expired); err == nil {
		t.Fatal("expired state was accepted")
	}
}
