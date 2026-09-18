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
//

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// The sudo endpoints let a logged-in user re-prove an existing factor and
// receive a short-lived "sudo" grant, which then authorizes sensitive
// MFA-management operations on accounts that have no usable password
// (social-login / passkey-only). This is the standard step-up-to-manage
// pattern ("sudo mode"): the assurance of a security change matches the
// factor being managed, and stays phishing-resistant for WebAuthn.

// issueSudo writes the sudo grant for the authenticated account
func (a *App) issueSudo(c *echo.Context, ownerPrn, factor string) error {
	token, err := mfaservice.CreateSudoToken(a.jwtConfig, ownerPrn, factor)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error creating reauth token", http.StatusInternalServerError)
	}
	noStore(c)
	return echoutil.WriteJSON(c, http.StatusOK, authmodels.SudoResponse{SudoToken: token})
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
func (a *App) handlePostReauthTOTP(c *echo.Context) error {
	if !mfaFeatureEnabled() {
		return echoutil.RestErrorWrapperUser(c, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
	}
	account, _, ok := a.mfaCaller(c)
	if !ok {
		return nil
	}

	payload := &authmodels.MFASudoCodeRequest{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode request", http.StatusBadRequest)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with database connectivity", http.StatusInternalServerError)
	}
	if !settings.HasConfirmedTOTP() {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}
	if settings.IsLocked(time.Now()) {
		return echoutil.RestErrorWrapperUser(c, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
	}

	secret, err := mfaservice.DecryptSecret(settings.TOTP.SecretEnc)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error reading TOTP secret", http.StatusInternalServerError)
	}

	step, valid := mfaservice.VerifyTOTPCode(secret, payload.Code, time.Now())
	if !valid {
		return a.mfaLoginFailure(c, account.Prn)
	}
	if err := a.mfaRepo.UseTOTPStep(ctx, account.Prn, step); err != nil {
		return a.mfaLoginFailure(c, account.Prn)
	}

	return a.issueSudo(c, account.Prn, "otp")
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
func (a *App) handlePostReauthRecovery(c *echo.Context) error {
	if !mfaFeatureEnabled() {
		return echoutil.RestErrorWrapperUser(c, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
	}
	account, _, ok := a.mfaCaller(c)
	if !ok {
		return nil
	}

	payload := &authmodels.MFASudoCodeRequest{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode request", http.StatusBadRequest)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}
	if settings.IsLocked(time.Now()) {
		return echoutil.RestErrorWrapperUser(c, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
	}

	index, valid := mfaservice.VerifyRecoveryCode(settings.RecoveryCodes, payload.Code)
	if !valid {
		return a.mfaLoginFailure(c, account.Prn)
	}
	if err := a.mfaRepo.UseRecoveryCode(ctx, account.Prn, index); err != nil {
		return a.mfaLoginFailure(c, account.Prn)
	}

	return a.issueSudo(c, account.Prn, "recovery")
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
func (a *App) handlePostReauthWebauthn(c *echo.Context) error {
	if !mfaFeatureEnabled() {
		return echoutil.RestErrorWrapperUser(c, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
	}
	account, _, ok := a.mfaCaller(c)
	if !ok {
		return nil
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	if settings.IsLocked(time.Now()) {
		return echoutil.RestErrorWrapperUser(c, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
	}

	user, creds, err := a.webauthnUserFor(ctx, account, settings)
	if err != nil || len(creds) == 0 {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	// "preferred" lets a plain second-factor key assert without UV while a
	// passkey (which had to verify the user to sign in) performs UV again;
	// the finish step then requires UV for passkeys only
	assertion, sessionData, err := wa.BeginLogin(user,
		webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error starting WebAuthn reauth", http.StatusInternalServerError)
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error starting WebAuthn reauth", http.StatusInternalServerError)
	}

	sessionID, err := a.webauthnRepo.CreateSession(ctx, account.Prn, storage.WebauthnPurposeLogin, false, sessionJSON, mfaservice.PendingTokenTimeout())
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with database connectivity", http.StatusInternalServerError)
	}

	noStore(c)
	return echoutil.WriteJSON(c, http.StatusOK, authmodels.WebauthnOptionsResponse{
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
func (a *App) handlePostReauthWebauthnFinish(c *echo.Context) error {
	if !mfaFeatureEnabled() {
		return echoutil.RestErrorWrapperUser(c, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
	}
	account, _, ok := a.mfaCaller(c)
	if !ok {
		return nil
	}

	payload := &authmodels.WebauthnSudoFinishRequest{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode request", http.StatusBadRequest)
	}

	wa, err := mfaservice.GetWebAuthn()
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, "WebAuthn is not configured on this server", "WebAuthn is not configured on this server", http.StatusNotImplemented)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	session, err := a.webauthnRepo.ConsumeSession(ctx, payload.SessionID, storage.WebauthnPurposeLogin)
	if err != nil || session.Owner != account.Prn {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	sessionData := webauthn.SessionData{}
	if err := json.Unmarshal(session.Data, &sessionData); err != nil {
		return echoutil.RestErrorWrapper(c, "Error reading WebAuthn session", http.StatusInternalServerError)
	}

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil || settings == nil {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	user, _, err := a.webauthnUserFor(ctx, account, settings)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with database connectivity", http.StatusInternalServerError)
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(payload.Credential))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid WebAuthn assertion response", http.StatusBadRequest)
	}

	credential, err := wa.ValidateLogin(user, sessionData, parsed)
	if err != nil {
		return a.mfaLoginFailure(c, account.Prn)
	}

	// a sudo grant authorises factor changes, so a passkey must not get one
	// with a weaker (possession-only) assertion than the one it signs in
	// with; plain second-factor keys are still accepted without UV
	if ok := a.acceptAssertedCredential(ctx, c, account.Prn, credential, true); !ok {
		return nil
	}

	return a.issueSudo(c, account.Prn, "webauthn")
}
