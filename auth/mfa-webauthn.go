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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ensureMFASettings loads the account's MFA settings, creating the initial
// doc (with its stable WebAuthn user handle) when none exists yet
func (a *App) ensureMFASettings(ctx context.Context, account *accounts.Account) (*storage.MFASettings, error) {
	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		return nil, err
	}
	if settings != nil {
		return settings, nil
	}

	userHandle := make([]byte, 32)
	if _, err := rand.Read(userHandle); err != nil {
		return nil, err
	}

	settings = &storage.MFASettings{
		Owner:      account.Prn,
		UserHandle: userHandle,
	}
	if err := a.mfaRepo.Upsert(ctx, settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// webauthnUserFor builds the ceremony user for an account
func (a *App) webauthnUserFor(ctx context.Context, account *accounts.Account, settings *storage.MFASettings) (*mfaservice.WebauthnUser, []storage.WebauthnCredential, error) {
	creds, err := a.webauthnRepo.ListByOwner(ctx, account.Prn)
	if err != nil {
		return nil, nil, err
	}

	name := account.Email
	if name == "" {
		name = account.Nick
	}

	return mfaservice.NewWebauthnUser(settings.UserHandle, name, account.Nick, creds), creds, nil
}

func credentialInfo(c *storage.WebauthnCredential) authmodels.WebauthnCredentialInfo {
	info := authmodels.WebauthnCredentialInfo{
		ID:          c.ID.Hex(),
		Name:        c.Name,
		IsPasskey:   c.IsPasskey,
		TimeCreated: c.TimeCreated.UTC().Format(time.RFC3339),
	}
	if c.LastUsedAt != nil {
		info.LastUsedAt = c.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return info
}

// @Summary Start registering a security key or passkey
// @Description Begins a WebAuthn registration ceremony. Requires the account password (or a fresh session for passwordless accounts). Set passkey to true to register a discoverable credential usable for passwordless sign-in.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.WebauthnRegisterRequest true "Fresh-auth proof and credential kind"
// @Success 200 {object} authmodels.WebauthnOptionsResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Failure 501 {object} utils.RError
// @Router /auth/mfa/webauthn/register [post]
func (a *App) handlePostWebauthnRegister(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, claims, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.WebauthnRegisterRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	if !a.freshAuthOK(account, payload.Password, payload.SudoToken, claims) {
		utils.RestErrorWrapperUser(writer, "Password verification failed", "Password verification failed", http.StatusUnauthorized)
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.ensureMFASettings(ctx, account)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	user, creds, err := a.webauthnUserFor(ctx, account, settings)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	exclusions := make([]protocol.CredentialDescriptor, 0, len(creds))
	for _, c := range creds {
		exclusions = append(exclusions, c.Credential.Descriptor())
	}

	creation, sessionData, err := wa.BeginRegistration(user, mfaservice.RegistrationOptions(payload.Passkey, exclusions)...)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting WebAuthn registration", http.StatusInternalServerError)
		return
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting WebAuthn registration", http.StatusInternalServerError)
		return
	}

	sessionID, err := a.webauthnRepo.CreateSession(ctx, account.Prn, storage.WebauthnPurposeRegister, payload.Passkey, sessionJSON, mfaservice.PendingTokenTimeout())
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.WebauthnOptionsResponse{
		SessionID: sessionID,
		Options:   creation,
	})
}

// @Summary Finish registering a security key or passkey
// @Description Verifies the authenticator's attestation response and stores the credential. Returns fresh recovery codes when this registration turned two-factor authentication on.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.WebauthnRegisterFinishRequest true "Session id, label and browser credential response"
// @Success 200 {object} authmodels.WebauthnRegisterFinishResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/webauthn/register/finish [post]
func (a *App) handlePostWebauthnRegisterFinish(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.WebauthnRegisterFinishRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	session, err := a.webauthnRepo.ConsumeSession(ctx, payload.SessionID, storage.WebauthnPurposeRegister)
	if err != nil || session.Owner != account.Prn {
		utils.RestErrorWrapperUser(writer, "Invalid or expired WebAuthn session", "Invalid or expired WebAuthn session", http.StatusUnauthorized)
		return
	}

	sessionData := webauthn.SessionData{}
	if err := json.Unmarshal(session.Data, &sessionData); err != nil {
		utils.RestErrorWrapper(writer, "Error reading WebAuthn session", http.StatusInternalServerError)
		return
	}

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	user, _, err := a.webauthnUserFor(ctx, account, settings)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(payload.Credential))
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Invalid WebAuthn credential response", "Invalid WebAuthn credential response", http.StatusBadRequest)
		return
	}

	credential, err := wa.CreateCredential(user, sessionData, parsed)
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn registration failed", "WebAuthn registration failed", http.StatusUnauthorized)
		return
	}

	name := payload.Name
	if name == "" {
		if session.IsPasskey {
			name = "Passkey"
		} else {
			name = "Security key"
		}
	}

	stored := &storage.WebauthnCredential{
		Owner:      account.Prn,
		Name:       name,
		IsPasskey:  session.IsPasskey && credential.Flags.UserVerified,
		Credential: *credential,
	}
	if err := a.webauthnRepo.CreateCredential(ctx, stored); err != nil {
		if err == storage.ErrMFAReplayed {
			utils.RestErrorWrapperUser(writer, "This authenticator is already registered", "This authenticator is already registered", http.StatusConflict)
			return
		}
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	// enable MFA on first factor; hand out recovery codes exactly once
	response := authmodels.WebauthnRegisterFinishResponse{
		Credential: credentialInfo(stored),
	}
	if !settings.Enabled || settings.RecoveryCodesRemaining() == 0 {
		plainCodes, hashedCodes, err := mfaservice.GenerateRecoveryCodes()
		if err != nil {
			utils.RestErrorWrapper(writer, "Error generating recovery codes", http.StatusInternalServerError)
			return
		}
		settings.Enabled = true
		settings.RecoveryCodes = hashedCodes
		if err := a.mfaRepo.Upsert(ctx, settings); err != nil {
			utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
			return
		}
		response.RecoveryCodes = plainCodes
	} else if !settings.Enabled {
		settings.Enabled = true
		if err := a.mfaRepo.Upsert(ctx, settings); err != nil {
			utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
			return
		}
	}

	noStore(writer)
	writer.WriteJson(response)
}

// @Summary Rename a registered security key or passkey
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param id path string true "Credential id"
// @Param body body authmodels.WebauthnRenameRequest true "New label"
// @Success 200 {object} authmodels.WebauthnCredentialInfo
// @Failure 400 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/webauthn/credentials/{id} [patch]
func (a *App) handlePatchWebauthnCredential(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.WebauthnRenameRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil || payload.Name == "" {
		utils.RestErrorWrapper(writer, "A name is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	id := r.PathParam("id")
	if err := a.webauthnRepo.RenameCredential(ctx, account.Prn, id, payload.Name); err != nil {
		utils.RestErrorWrapperUser(writer, "Credential not found", "Credential not found", http.StatusNotFound)
		return
	}

	writer.WriteJson(map[string]interface{}{"id": id, "name": payload.Name})
}

// @Summary Remove a registered security key or passkey
// @Description Removes a credential. Requires the account password (or a fresh session for passwordless accounts). Disables two-factor authentication when no other factor remains.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param id path string true "Credential id"
// @Param body body authmodels.MFAPasswordRequest true "Fresh-auth proof"
// @Success 204
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/webauthn/credentials/{id} [delete]
func (a *App) handleDeleteWebauthnCredential(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, claims, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.MFAPasswordRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil && err != rest.ErrJsonPayloadEmpty {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	if !a.freshAuthOK(account, payload.Password, payload.SudoToken, claims) {
		utils.RestErrorWrapperUser(writer, "Password verification failed", "Password verification failed", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	if err := a.webauthnRepo.DeleteCredential(ctx, account.Prn, r.PathParam("id")); err != nil {
		utils.RestErrorWrapperUser(writer, "Credential not found", "Credential not found", http.StatusNotFound)
		return
	}

	// last factor gone -> disable MFA and invalidate recovery codes. If the
	// post-delete bookkeeping can't be read, don't report success: the
	// account would be left enabled with no usable factor, advertising
	// protection it no longer has. Surface a 503 so the client retries.
	count, err := a.webauthnRepo.CountByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Error with database connectivity", "Please try again later", http.StatusServiceUnavailable)
		return
	}
	if count == 0 {
		settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
		if err != nil {
			utils.RestErrorWrapperUser(writer, "Error with database connectivity", "Please try again later", http.StatusServiceUnavailable)
			return
		}
		if settings != nil && !settings.HasConfirmedTOTP() {
			if err := a.mfaRepo.SetEnabled(ctx, account.Prn, false); err != nil {
				utils.RestErrorWrapperUser(writer, "Error with database connectivity", "Please try again later", http.StatusServiceUnavailable)
				return
			}
		}
	}

	writer.WriteHeader(http.StatusNoContent)
}

// @Summary Start the WebAuthn second factor of a pending login
// @Description Returns assertion options for the account's registered security keys/passkeys. Authenticated by the mfa_token from POST /auth/login.
// @Accept json
// @Produce json
// @Tags auth
// @Param body body authmodels.WebauthnLoginRequest true "Pending token"
// @Success 200 {object} authmodels.WebauthnOptionsResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/login/mfa/webauthn [post]
func (a *App) handlePostLoginMFAWebauthn(writer rest.ResponseWriter, r *rest.Request) {
	payload := &authmodels.WebauthnLoginRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	claims, settings, ok := a.mfaPendingFromRequest(writer, r, payload.MFAToken)
	if !ok {
		return
	}

	if !claims.HasMethod(authmodels.MFAMethodWebauthn) {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	account, err := authservices.GetAccount(claims.Prn, a.mongoClient)
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	user, creds, err := a.webauthnUserFor(ctx, &account, settings)
	if err != nil || len(creds) == 0 {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	// step-up assertion: possession only, no PIN prompt - the password was
	// the knowledge factor (passkey sign-in keeps UV required instead)
	assertion, sessionData, err := wa.BeginLogin(user,
		webauthn.WithUserVerification(protocol.VerificationDiscouraged))
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting WebAuthn login", http.StatusInternalServerError)
		return
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting WebAuthn login", http.StatusInternalServerError)
		return
	}

	sessionID, err := a.webauthnRepo.CreateSession(ctx, claims.Prn, storage.WebauthnPurposeLogin, false, sessionJSON, mfaservice.PendingTokenTimeout())
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.WebauthnOptionsResponse{
		SessionID: sessionID,
		Options:   assertion,
	})
}

// @Summary Complete a pending login with a security key or passkey
// @Description Verifies the WebAuthn assertion and exchanges the mfa_token for a session token.
// @Accept json
// @Produce json
// @Tags auth
// @Param body body authmodels.WebauthnLoginFinishRequest true "Pending token, session id and browser assertion response"
// @Success 200 {object} authmodels.TokenResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/login/mfa/webauthn/finish [post]
func (a *App) handlePostLoginMFAWebauthnFinish(writer rest.ResponseWriter, r *rest.Request) {
	payload := &authmodels.WebauthnLoginFinishRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	claims, settings, ok := a.mfaPendingFromRequest(writer, r, payload.MFAToken)
	if !ok {
		return
	}

	if !claims.HasMethod(authmodels.MFAMethodWebauthn) {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	session, err := a.webauthnRepo.ConsumeSession(ctx, payload.SessionID, storage.WebauthnPurposeLogin)
	if err != nil || session.Owner != claims.Prn {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	sessionData := webauthn.SessionData{}
	if err := json.Unmarshal(session.Data, &sessionData); err != nil {
		utils.RestErrorWrapper(writer, "Error reading WebAuthn session", http.StatusInternalServerError)
		return
	}

	account, err := authservices.GetAccount(claims.Prn, a.mongoClient)
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	user, _, err := a.webauthnUserFor(ctx, &account, settings)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(payload.Credential))
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Invalid WebAuthn assertion response", "Invalid WebAuthn assertion response", http.StatusBadRequest)
		return
	}

	credential, err := wa.ValidateLogin(user, sessionData, parsed)
	if err != nil {
		a.mfaLoginFailure(writer, r, claims.Prn)
		return
	}

	if ok := a.acceptAssertedCredential(ctx, writer, r, claims.Prn, credential); !ok {
		return
	}

	a.mfaLoginSuccess(writer, r, claims, "webauthn")
}

// acceptAssertedCredential applies the post-assertion bookkeeping shared by
// the 2FA and passkey flows: clone detection on the sign counter and
// persisting the updated authenticator state. Non-backup-eligible
// credentials with a counter regression are treated as cloned and rejected;
// synced (backup-eligible) passkeys legitimately report non-incrementing
// counters and only log.
func (a *App) acceptAssertedCredential(ctx context.Context, writer rest.ResponseWriter, r *rest.Request, ownerPrn string, credential *webauthn.Credential) bool {
	stored, err := a.webauthnRepo.GetByCredentialID(ctx, credential.ID)
	if err != nil || stored == nil || stored.Owner != ownerPrn {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return false
	}

	// A credential registered as a passkey carries user verification as its
	// own factor; an assertion from it that is not user-verified is either a
	// downgrade or a different (2FA-only) authenticator asserting the same id,
	// and must not be accepted with passkey-level assurance. Second-factor
	// keys legitimately assert without UV (the password/step-up is the other
	// factor), so this only applies to passkeys.
	if stored.IsPasskey && !credential.Flags.UserVerified {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return false
	}

	if credential.Authenticator.CloneWarning {
		log.Printf("WARN: webauthn clone warning for credential %s of %s (backup_eligible=%v)\n",
			stored.ID.Hex(), ownerPrn, credential.Flags.BackupEligible)
		if !credential.Flags.BackupEligible {
			utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
			return false
		}
	}

	// prior counter the assertion validated against; the compare-and-set in
	// UpdateCredentialAfterLogin rejects a racing assertion that shares it
	prevSignCount := stored.Credential.Authenticator.SignCount
	stored.Credential = *credential
	if err := a.webauthnRepo.UpdateCredentialAfterLogin(ctx, stored, prevSignCount); err != nil {
		// a non-synced authenticator that cannot durably advance its counter
		// (lost CAS race, or write failure) must not be allowed to log in:
		// that is exactly the clone/replay case the counter defends against.
		// Synced passkeys (backup-eligible) have no meaningful counter, so a
		// persistence hiccup there only warrants a warning.
		log.Printf("WARN: could not persist webauthn credential state for %s: %s\n", ownerPrn, err.Error())
		if !credential.Flags.BackupEligible {
			utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
			return false
		}
	}

	return true
}

// @Summary Start a passkey sign-in
// @Description Begins a usernameless WebAuthn ceremony with discoverable credentials. User verification is required; a successful passkey assertion counts as multi-factor on its own.
// @Accept json
// @Produce json
// @Tags auth
// @Success 200 {object} authmodels.WebauthnOptionsResponse
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Failure 501 {object} utils.RError
// @Router /auth/login/webauthn/begin [post]
func (a *App) handlePostPasskeyLoginBegin(writer rest.ResponseWriter, r *rest.Request) {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return
	}

	if !mfaFeatureEnabled() || a.mfaRepo == nil {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	assertion, sessionData, err := wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting passkey login", http.StatusInternalServerError)
		return
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting passkey login", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	sessionID, err := a.webauthnRepo.CreateSession(ctx, "", storage.WebauthnPurposePasskey, true, sessionJSON, mfaservice.PendingTokenTimeout())
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.WebauthnOptionsResponse{
		SessionID: sessionID,
		Options:   assertion,
	})
}

// @Summary Complete a passkey sign-in
// @Description Verifies the discoverable-credential assertion, resolves the account from the asserted user handle and mints a session token. No password involved.
// @Accept json
// @Produce json
// @Tags auth
// @Param body body authmodels.PasskeyLoginFinishRequest true "Session id and browser assertion response"
// @Success 200 {object} authmodels.TokenResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/login/webauthn/finish [post]
func (a *App) handlePostPasskeyLoginFinish(writer rest.ResponseWriter, r *rest.Request) {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return
	}

	if !mfaFeatureEnabled() || a.mfaRepo == nil {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	payload := &authmodels.PasskeyLoginFinishRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	session, err := a.webauthnRepo.ConsumeSession(ctx, payload.SessionID, storage.WebauthnPurposePasskey)
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	sessionData := webauthn.SessionData{}
	if err := json.Unmarshal(session.Data, &sessionData); err != nil {
		utils.RestErrorWrapper(writer, "Error reading WebAuthn session", http.StatusInternalServerError)
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(payload.Credential))
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Invalid WebAuthn assertion response", "Invalid WebAuthn assertion response", http.StatusBadRequest)
		return
	}

	var account accounts.Account

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		settings, err := a.mfaRepo.GetByUserHandle(ctx, userHandle)
		if err != nil || settings == nil {
			return nil, mfaservice.ErrInvalidPendingToken
		}

		account, err = authservices.GetAccount(settings.Owner, a.mongoClient)
		if err != nil {
			return nil, err
		}

		user, _, err := a.webauthnUserFor(ctx, &account, settings)
		return user, err
	}

	credential, err := wa.ValidateDiscoverableLogin(handler, sessionData, parsed)
	if err != nil || account.Prn == "" {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	if !credential.Flags.UserVerified {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	if ok := a.acceptAssertedCredential(ctx, writer, r, account.Prn, credential); !ok {
		return
	}

	tokenPayload := &authmodels.LoginRequestPayload{
		Username: account.Nick,
	}
	extraClaims := map[string]interface{}{
		"amr":       []string{"webauthn", "uv"},
		"auth_time": time.Now().Unix(),
	}

	tokenString, rerr := authservices.MintAuthenticatedUserToken(tokenPayload, extraClaims, a.jwtMiddleware, a.mongoClient)
	if rerr != nil {
		utils.RestErrorWrite(writer, rerr)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.TokenResponse{
		Token: tokenString,
	})
}
