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
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ErrWebauthnNotConfigured RP ID / origins envs are missing
var ErrWebauthnNotConfigured = errors.New("webauthn is not configured on this server")

// GetWebAuthn builds the relying party from the deployment configuration.
// RPID and RPOrigins are deployment-specific and deliberately have no
// defaults: hub UIs run at multiple origins and credentials are scoped to
// the RP ID, so guessing wrong would strand user credentials.
func GetWebAuthn() (*webauthn.WebAuthn, error) {
	// canonicalize the RP ID to a lower-case domain with no trailing dot so
	// the host comparisons below match the browser's canonical RP tuple
	rpID := strings.TrimSuffix(strings.ToLower(utils.GetEnv(utils.EnvPantahubWebauthnRPID)), ".")
	origins := strings.Split(utils.GetEnv(utils.EnvPantahubWebauthnRPOrigins), ",")

	cleanOrigins := []string{}
	for _, o := range origins {
		if o = strings.TrimSpace(o); o != "" {
			cleanOrigins = append(cleanOrigins, o)
		}
	}

	if rpID == "" || len(cleanOrigins) == 0 {
		return nil, ErrWebauthnNotConfigured
	}

	// the RPID is a registrable domain, never a URL; catching a scheme or
	// path here beats stranding every credential registered under a bad id
	if strings.Contains(rpID, "://") || strings.ContainsAny(rpID, "/ :") {
		return nil, fmt.Errorf("webauthn RPID must be a bare domain, got %q", rpID)
	}

	// Every origin becomes a credential trust boundary, so a misconfiguration
	// (a typo, a plain-http origin, a host outside the RP ID) must fail the
	// server rather than silently trusting an unauthenticated origin. Each
	// entry must be an exact origin: https (or http only for loopback dev),
	// no userinfo/path/query/fragment, host == RPID or a subdomain of it.
	for _, o := range cleanOrigins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || u.User != nil ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("webauthn origin must be a bare scheme://host[:port], got %q", o)
		}

		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback) {
			return nil, fmt.Errorf("webauthn origin %q must use https (http allowed only for loopback)", o)
		}

		if host != rpID && !strings.HasSuffix(host, "."+rpID) && !isLoopback {
			return nil, fmt.Errorf("webauthn origin host %q is not the RP ID %q or a subdomain of it", host, rpID)
		}
	}

	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: utils.GetEnv(utils.EnvPantahubWebauthnRPName),
		RPOrigins:     cleanOrigins,
	})
}

// WebauthnUser adapts an account plus its stored credentials to the
// webauthn.User interface. The ID is the stable random user handle from the
// MFA settings, never the account PRN (the spec requires an opaque value).
type WebauthnUser struct {
	Handle      []byte
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
}

// WebAuthnID implements webauthn.User
func (u *WebauthnUser) WebAuthnID() []byte { return u.Handle }

// WebAuthnName implements webauthn.User
func (u *WebauthnUser) WebAuthnName() string { return u.Name }

// WebAuthnDisplayName implements webauthn.User
func (u *WebauthnUser) WebAuthnDisplayName() string { return u.DisplayName }

// WebAuthnCredentials implements webauthn.User
func (u *WebauthnUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// NewWebauthnUser builds the ceremony user for an account
func NewWebauthnUser(userHandle []byte, name, displayName string, creds []storage.WebauthnCredential) *WebauthnUser {
	credentials := make([]webauthn.Credential, 0, len(creds))
	for _, c := range creds {
		credentials = append(credentials, c.Credential)
	}
	if displayName == "" {
		displayName = name
	}
	return &WebauthnUser{
		Handle:      userHandle,
		Name:        name,
		DisplayName: displayName,
		Credentials: credentials,
	}
}

// RegistrationOptions returns the ceremony options for registering a new
// credential. Passkeys require a discoverable credential with user
// verification (the UV bit is what makes a passkey multi-factor on its own);
// plain second-factor keys stay non-resident so hardware key slots are not
// wasted.
func RegistrationOptions(isPasskey bool, exclusions []protocol.CredentialDescriptor) []webauthn.RegistrationOption {
	opts := []webauthn.RegistrationOption{
		webauthn.WithExclusions(exclusions),
	}

	if isPasskey {
		opts = append(opts,
			webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
			webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
				ResidentKey:      protocol.ResidentKeyRequirementRequired,
				UserVerification: protocol.VerificationRequired,
			}),
		)
	} else {
		// second-factor keys: the password already covered the knowledge
		// factor, so the key only proves possession - one touch, no PIN
		// (same policy GitLab/GitHub use for 2FA ceremonies)
		opts = append(opts,
			webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementDiscouraged),
			webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
				ResidentKey:      protocol.ResidentKeyRequirementDiscouraged,
				UserVerification: protocol.VerificationDiscouraged,
			}),
		)
	}

	return opts
}
