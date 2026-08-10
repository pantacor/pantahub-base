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
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func TestGetWebAuthn(t *testing.T) {
	t.Run("unconfigured returns ErrWebauthnNotConfigured", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "")
		t.Setenv(utils.EnvPantahubWebauthnRPOrigins, "")
		if _, err := GetWebAuthn(); err != ErrWebauthnNotConfigured {
			t.Errorf("expected ErrWebauthnNotConfigured, got %v", err)
		}
	})

	t.Run("missing origins returns ErrWebauthnNotConfigured", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "pantacor.com")
		t.Setenv(utils.EnvPantahubWebauthnRPOrigins, " , ")
		if _, err := GetWebAuthn(); err != ErrWebauthnNotConfigured {
			t.Errorf("expected ErrWebauthnNotConfigured, got %v", err)
		}
	})

	t.Run("valid config builds relying party", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "pantacor.com")
		t.Setenv(utils.EnvPantahubWebauthnRPOrigins, "https://hub.pantacor.com, https://hub2.pantacor.com")
		wa, err := GetWebAuthn()
		if err != nil {
			t.Fatal(err)
		}
		if wa.Config.RPID != "pantacor.com" {
			t.Errorf("unexpected RPID: %s", wa.Config.RPID)
		}
		if len(wa.Config.RPOrigins) != 2 || wa.Config.RPOrigins[1] != "https://hub2.pantacor.com" {
			t.Errorf("origins must be split and trimmed: %v", wa.Config.RPOrigins)
		}
	})

	t.Run("invalid rpid rejected", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "https://pantacor.com")
		t.Setenv(utils.EnvPantahubWebauthnRPOrigins, "https://hub.pantacor.com")
		if _, err := GetWebAuthn(); err == nil {
			t.Error("an RPID with a scheme must be rejected")
		}
	})

	t.Run("unsafe origins rejected", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "pantacor.com")
		for _, bad := range []string{
			"http://hub.pantacor.com",           // plain http on a real host
			"https://hub.evil.com",              // host outside the RP ID
			"https://hub.pantacor.com/callback", // carries a path
			"https://user@hub.pantacor.com",     // userinfo
			"https://hub.pantacor.com?x=1",      // query
			"not-a-url",                         // unparseable
		} {
			t.Setenv(utils.EnvPantahubWebauthnRPOrigins, bad)
			if _, err := GetWebAuthn(); err == nil {
				t.Errorf("origin %q must be rejected", bad)
			}
		}
	})

	t.Run("loopback http allowed for dev", func(t *testing.T) {
		t.Setenv(utils.EnvPantahubWebauthnRPID, "localhost")
		t.Setenv(utils.EnvPantahubWebauthnRPOrigins, "http://localhost:3000")
		if _, err := GetWebAuthn(); err != nil {
			t.Errorf("loopback http origin must be allowed: %v", err)
		}
	})
}

func TestRegistrationOptions(t *testing.T) {
	t.Setenv(utils.EnvPantahubWebauthnRPID, "pantacor.com")
	t.Setenv(utils.EnvPantahubWebauthnRPOrigins, "https://hub.pantacor.com")

	wa, err := GetWebAuthn()
	if err != nil {
		t.Fatal(err)
	}

	user := NewWebauthnUser([]byte("0123456789abcdef0123456789abcdef"), "user@example.com", "user1", nil)

	t.Run("passkey requires resident key and uv", func(t *testing.T) {
		creation, _, err := wa.BeginRegistration(user, RegistrationOptions(true, nil)...)
		if err != nil {
			t.Fatal(err)
		}
		sel := creation.Response.AuthenticatorSelection
		if sel.ResidentKey != protocol.ResidentKeyRequirementRequired {
			t.Errorf("passkey must require a resident key, got %q", sel.ResidentKey)
		}
		if sel.UserVerification != protocol.VerificationRequired {
			t.Errorf("passkey must require user verification, got %q", sel.UserVerification)
		}
	})

	t.Run("second factor key stays non-resident", func(t *testing.T) {
		creation, _, err := wa.BeginRegistration(user, RegistrationOptions(false, nil)...)
		if err != nil {
			t.Fatal(err)
		}
		sel := creation.Response.AuthenticatorSelection
		if sel.ResidentKey == protocol.ResidentKeyRequirementRequired {
			t.Error("second-factor key must not require a resident key")
		}
	})

	t.Run("exclusions are passed through", func(t *testing.T) {
		exclusions := []protocol.CredentialDescriptor{{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: []byte("credential-1"),
		}}
		creation, _, err := wa.BeginRegistration(user, RegistrationOptions(false, exclusions)...)
		if err != nil {
			t.Fatal(err)
		}
		if len(creation.Response.CredentialExcludeList) != 1 {
			t.Errorf("expected 1 excluded credential, got %d", len(creation.Response.CredentialExcludeList))
		}
	})
}

func TestWebauthnUser(t *testing.T) {
	handle := []byte("0123456789abcdef0123456789abcdef")
	creds := []storage.WebauthnCredential{
		{Credential: webauthn.Credential{ID: []byte("cred-1")}},
		{Credential: webauthn.Credential{ID: []byte("cred-2")}},
	}

	user := NewWebauthnUser(handle, "user@example.com", "", creds)

	if string(user.WebAuthnID()) != string(handle) {
		t.Error("user handle mismatch")
	}
	if user.WebAuthnName() != "user@example.com" {
		t.Errorf("unexpected name: %s", user.WebAuthnName())
	}
	if user.WebAuthnDisplayName() != "user@example.com" {
		t.Errorf("display name must fall back to name, got %s", user.WebAuthnDisplayName())
	}
	if len(user.WebAuthnCredentials()) != 2 {
		t.Errorf("expected 2 credentials, got %d", len(user.WebAuthnCredentials()))
	}
}
