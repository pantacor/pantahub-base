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

package tokenmodels

import "testing"

func TestHashSecretIsDeterministicHexSHA256(t *testing.T) {
	h1 := HashSecret("abc")
	h2 := HashSecret("abc")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(h1))
	}
	// sha256("abc") well-known vector
	if h1 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("unexpected digest %s", h1)
	}
}

func TestSecretMatches(t *testing.T) {
	tok := &AuthToken{SecretHash: HashSecret("the-secret")}

	if !tok.SecretMatches("the-secret") {
		t.Fatal("expected secret to match its own hash")
	}
	if tok.SecretMatches("the-secret ") {
		t.Fatal("expected a different secret not to match")
	}
	if tok.SecretMatches("") {
		t.Fatal("empty presented secret must never match")
	}
	if tok.SecretMatches(tok.SecretHash) {
		t.Fatal("presenting the stored digest itself must not authenticate")
	}

	noHash := &AuthToken{Secret: "plaintext-only"}
	if noHash.SecretMatches("plaintext-only") {
		t.Fatal("a token without a stored hash must never match")
	}

	var nilTok *AuthToken
	if nilTok.SecretMatches("x") {
		t.Fatal("nil token must not match")
	}
}
