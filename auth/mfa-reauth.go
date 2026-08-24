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
//

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// The sudo endpoints let a logged-in user re-prove an existing factor and
// receive a short-lived "sudo" grant, which then authorizes sensitive
// MFA-management operations on accounts that have no usable password
// (social-login / passkey-only). This is the standard step-up-to-manage
// pattern ("sudo mode"): the assurance of a security change matches the
// factor being managed, and stays phishing-resistant for WebAuthn.

// issueSudo writes the sudo grant for the authenticated account
func (a *App) issueSudo(writer rest.ResponseWriter, ownerPrn, factor string) {
	token, err := mfaservice.CreateSudoToken(a.jwtMiddleware, ownerPrn, factor)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error creating reauth token", http.StatusInternalServerError)
		return
	}
	noStore(writer)
	writer.WriteJson(authmodels.SudoResponse{SudoToken: token})
}

// @Summary Re-authenticate with an authenticator (TOTP) code
// @Description Re-proves an existing TOTP factor and returns a short-lived sudo token used to authorize sensitive MFA-management operations.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.MFASudoCodeRequest true "Authenticator code"
// @Success 200 {object} authmodels.SudoResponse
// @Failure 401 {object} utils.RError
// @Router /auth/mfa/reauth/totp [post]
func (a *App) handlePostReauthTOTP(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}
	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.MFASudoCodeRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}
	if !settings.HasConfirmedTOTP() {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}
	if settings.IsLocked(time.Now()) {
		utils.RestErrorWrapperUser(writer, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
		return
	}

	secret, err := mfaservice.DecryptSecret(settings.TOTP.SecretEnc)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error reading TOTP secret", http.StatusInternalServerError)
		return
	}

	step, valid := mfaservice.VerifyTOTPCode(secret, payload.Code, time.Now())
	if !valid {
		a.mfaLoginFailure(writer, r, account.Prn)
		return
	}
	if err := a.mfaRepo.UseTOTPStep(ctx, account.Prn, step); err != nil {
		a.mfaLoginFailure(writer, r, account.Prn)
		return
	}

	a.issueSudo(writer, account.Prn, "otp")
}

// @Summary Re-authenticate with a recovery code
// @Description Re-proves the account with a single-use recovery code and returns a short-lived sudo token.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.MFASudoCodeRequest true "Recovery code"
// @Success 200 {object} authmodels.SudoResponse
// @Failure 401 {object} utils.RError
// @Router /auth/mfa/reauth/recovery [post]
func (a *App) handlePostReauthRecovery(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}
	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.MFASudoCodeRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}
	if settings.IsLocked(time.Now()) {
		utils.RestErrorWrapperUser(writer, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
		return
	}

	index, valid := mfaservice.VerifyRecoveryCode(settings.RecoveryCodes, payload.Code)
	if !valid {
		a.mfaLoginFailure(writer, r, account.Prn)
		return
	}
	if err := a.mfaRepo.UseRecoveryCode(ctx, account.Prn, index); err != nil {
		a.mfaLoginFailure(writer, r, account.Prn)
		return
	}

	a.issueSudo(writer, account.Prn, "recovery")
}

// @Summary Start a WebAuthn re-authentication
// @Description Returns assertion options for the caller's registered security keys/passkeys; completing it yields a sudo token for sensitive MFA-management operations.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Success 200 {object} authmodels.WebauthnOptionsResponse
// @Failure 401 {object} utils.RError
// @Router /auth/mfa/reauth/webauthn [post]
func (a *App) handlePostReauthWebauthn(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}
	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		utils.RestErrorWrapperUser(writer, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	if settings.IsLocked(time.Now()) {
		utils.RestErrorWrapperUser(writer, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
		return
	}

	user, creds, err := a.webauthnUserFor(ctx, account, settings)
	if err != nil || len(creds) == 0 {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	// "preferred" lets a plain second-factor key assert without UV while a
	// passkey (which had to verify the user to sign in) performs UV again;
	// the finish step then requires UV for passkeys only
	assertion, sessionData, err := wa.BeginLogin(user,
		webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting WebAuthn reauth", http.StatusInternalServerError)
		return
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error starting WebAuthn reauth", http.StatusInternalServerError)
		return
	}

	sessionID, err := a.webauthnRepo.CreateSession(ctx, account.Prn, storage.WebauthnPurposeLogin, false, sessionJSON, mfaservice.PendingTokenTimeout())
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

// @Summary Finish a WebAuthn re-authentication
// @Description Verifies the WebAuthn assertion and returns a short-lived sudo token.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.WebauthnSudoFinishRequest true "Session id and browser assertion"
// @Success 200 {object} authmodels.SudoResponse
// @Failure 401 {object} utils.RError
// @Router /auth/mfa/reauth/webauthn/finish [post]
func (a *App) handlePostReauthWebauthnFinish(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}
	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.WebauthnSudoFinishRequest{}
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

	session, err := a.webauthnRepo.ConsumeSession(ctx, payload.SessionID, storage.WebauthnPurposeLogin)
	if err != nil || session.Owner != account.Prn {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	sessionData := webauthn.SessionData{}
	if err := json.Unmarshal(session.Data, &sessionData); err != nil {
		utils.RestErrorWrapper(writer, "Error reading WebAuthn session", http.StatusInternalServerError)
		return
	}

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	user, _, err := a.webauthnUserFor(ctx, account, settings)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(payload.Credential))
	if err != nil {
		utils.RestErrorWrapper(writer, "Invalid WebAuthn assertion response", http.StatusBadRequest)
		return
	}

	credential, err := wa.ValidateLogin(user, sessionData, parsed)
	if err != nil {
		a.mfaLoginFailure(writer, r, account.Prn)
		return
	}

	// a sudo grant authorises factor changes, so a passkey must not get one
	// with a weaker (possession-only) assertion than the one it signs in
	// with; plain second-factor keys are still accepted without UV
	if ok := a.acceptAssertedCredential(ctx, writer, r, account.Prn, credential, true); !ok {
		return
	}

	a.issueSudo(writer, account.Prn, "webauthn")
}
