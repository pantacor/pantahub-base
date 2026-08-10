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
	"context"
	"crypto/rand"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const mfaFreshAuthWindow = 5 * time.Minute

// mfaFeatureEnabled tells if the MFA endpoints are active on this deployment
func mfaFeatureEnabled() bool {
	return utils.GetEnv(utils.EnvPantahubMfaEnabled) == "true"
}

// noStore marks a response as holding freshly issued authentication material
// (TOTP secret, recovery codes, session/pending tokens) so no cache,
// service worker or proxy retains it.
func noStore(writer rest.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store, no-cache, max-age=0")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Referrer-Policy", "no-referrer")
}

// mfaCaller resolves the authenticated USER account behind a management
// request. Non-USER callers (devices, services, sessions, clients) get a 403.
func (a *App) mfaCaller(writer rest.ResponseWriter, r *rest.Request) (*accounts.Account, jwtgo.MapClaims, bool) {
	authInfo := utils.GetAuthInfo(r)
	if authInfo == nil || authInfo.CallerType != "USER" {
		utils.RestErrorWrapperUser(writer, "Only users can manage two-factor authentication", "Only users can manage two-factor authentication", http.StatusForbidden)
		return nil, nil, false
	}

	claims, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)
	if !ok {
		utils.RestErrorWrapper(writer, "You need to be logged in", http.StatusForbidden)
		return nil, nil, false
	}

	account, err := authservices.GetAccount(string(authInfo.Caller), a.mongoClient)
	if err != nil {
		utils.RestErrorWrapper(writer, "Account not found", http.StatusForbidden)
		return nil, nil, false
	}

	return &account, claims, true
}

// accountHasPassword tells whether the account can prove itself with a
// password. Social-login and passkey-only accounts carry only a random
// password they were never told, so for them the password path is unusable.
func accountHasPassword(account *accounts.Account) bool {
	return account.PasswordBcrypt != "" || account.Password != ""
}

// checkPassword verifies a supplied password against the account
func checkPassword(account *accounts.Account, password string) bool {
	if password == "" {
		return false
	}
	if account.PasswordBcrypt != "" &&
		utils.CheckPasswordHash(password, account.PasswordBcrypt, utils.CryptoMethods.BCrypt) {
		return true
	}
	return account.Password != "" && password == account.Password
}

// hasAnyFactor tells whether the account has at least one confirmed second
// factor (TOTP or a registered WebAuthn credential). It returns an error
// rather than a bare bool so callers can distinguish "no factor" from
// "could not tell" and fail closed on the latter - conflating the two would
// let a factor-store outage downgrade a protected account to a bare-session
// bootstrap.
func (a *App) hasAnyFactor(ctx context.Context, ownerPrn string) (bool, error) {
	settings, err := a.mfaRepo.GetByOwner(ctx, ownerPrn)
	if err != nil {
		return false, err
	}
	if settings.HasConfirmedTOTP() {
		return true, nil
	}
	if a.webauthnRepo != nil {
		n, err := a.webauthnRepo.CountByOwner(ctx, ownerPrn)
		if err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// freshAuthOK verifies the fresh-auth proof required for a sensitive MFA
// operation. The order of preference is:
//
//   - the account password (accounts that have one);
//   - a valid sudo token proving the user just re-authenticated with an
//     existing factor (the standard "sudo mode" step-up);
//   - for accounts with NO usable password AND no factor yet to step up to
//     (first-factor enrollment), a genuinely fresh session (orig_iat within
//     mfaFreshAuthWindow) - you cannot re-prove a factor that does not exist.
//
// A passwordless account that already has a factor therefore CANNOT authorize
// a change with a bare session: it must present a sudo token. This closes the
// window where a stolen social/passkey session could add or remove factors.
func (a *App) freshAuthOK(account *accounts.Account, password, sudoToken string, claims jwtgo.MapClaims) bool {
	if accountHasPassword(account) {
		return checkPassword(account, password)
	}

	if sudoToken != "" {
		claimsSudo, err := mfaservice.ParseSudoToken(a.jwtMiddleware, sudoToken)
		if err == nil && claimsSudo.Prn == account.Prn {
			return true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// passwordless account: a bare fresh session only suffices to bootstrap
	// the very first factor; once any factor exists a sudo proof is required.
	// Fail closed if we cannot determine the factor state.
	hasFactor, err := a.hasAnyFactor(ctx, account.Prn)
	if err != nil || hasFactor {
		return false
	}

	return sessionIsFresh(claims)
}

// sessionIsFresh tells whether the session's orig_iat is within the fresh-auth
// window
func sessionIsFresh(claims jwtgo.MapClaims) bool {
	origIat, ok := claims["orig_iat"]
	if !ok {
		return false
	}

	var iat int64
	switch v := origIat.(type) {
	case float64:
		iat = int64(v)
	case int64:
		iat = v
	default:
		return false
	}

	return time.Since(time.Unix(iat, 0)) < mfaFreshAuthWindow
}

// @Summary Get the caller's two-factor authentication status
// @Description Returns whether MFA is enabled and which factors are configured for the authenticated user.
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Success 200 {object} authmodels.MFAStatusResponse
// @Failure 403 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa [get]
func (a *App) handleGetMFAStatus(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	response := authmodels.MFAStatusResponse{
		Webauthn:    []authmodels.WebauthnCredentialInfo{},
		PasswordSet: accountHasPassword(account),
	}
	if settings != nil {
		response.MFAEnabled = settings.Enabled
		response.TOTP.Enabled = settings.HasConfirmedTOTP()
		response.TOTP.Pending = settings.TOTP != nil && !settings.TOTP.Confirmed
		response.RecoveryCodesRemaining = settings.RecoveryCodesRemaining()
	}

	creds, err := a.webauthnRepo.ListByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}
	for i := range creds {
		response.Webauthn = append(response.Webauthn, credentialInfo(&creds[i]))
	}

	writer.WriteJson(response)
}

// @Summary Start a TOTP (authenticator app) enrollment
// @Description Generates a fresh TOTP secret for the authenticated user. Requires the account password (or a fresh session for passwordless accounts). The enrollment stays pending until confirmed with a first valid code.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.MFAPasswordRequest true "Fresh-auth proof"
// @Success 200 {object} authmodels.TOTPEnrollResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 409 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/totp [post]
func (a *App) handlePostTOTPEnroll(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, claims, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.MFAPasswordRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	if !a.freshAuthOK(account, payload.Password, payload.SudoToken, claims) {
		utils.RestErrorWrapperUser(writer, "Password verification failed", "Password verification failed", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	if settings.HasConfirmedTOTP() {
		utils.RestErrorWrapperUser(writer, "An authenticator app is already enrolled; remove it first", "An authenticator app is already enrolled; remove it first", http.StatusConflict)
		return
	}

	accountName := account.Email
	if accountName == "" {
		accountName = account.Nick
	}

	key, err := mfaservice.GenerateTOTPKey(utils.GetEnv(utils.EnvPantahubProductName), accountName)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error generating TOTP secret", http.StatusInternalServerError)
		return
	}

	secretEnc, err := mfaservice.EncryptSecret(key.Secret())
	if err == mfaservice.ErrMFANotConfigured {
		utils.RestErrorWrapperUser(writer, "MFA is not configured on this server", "MFA is not configured on this server", http.StatusNotImplemented)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(writer, "Error protecting TOTP secret", http.StatusInternalServerError)
		return
	}

	if settings == nil {
		userHandle := make([]byte, 32)
		if _, err := rand.Read(userHandle); err != nil {
			utils.RestErrorWrapper(writer, "Error generating user handle", http.StatusInternalServerError)
			return
		}
		settings = &storage.MFASettings{
			Owner:      account.Prn,
			UserHandle: userHandle,
		}
	}

	settings.TOTP = &storage.TOTPFactor{
		SecretEnc: secretEnc,
		Confirmed: false,
		CreatedAt: time.Now(),
	}

	if err := a.mfaRepo.Upsert(ctx, settings); err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.TOTPEnrollResponse{
		Secret:     key.Secret(),
		OtpauthURL: key.URL(),
	})
}

// @Summary Confirm a pending TOTP enrollment
// @Description Verifies the first code from the authenticator app, enables two-factor authentication and returns the single-use recovery codes (shown exactly once).
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.TOTPConfirmRequest true "First authenticator code"
// @Success 200 {object} authmodels.RecoveryCodesResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/totp/confirm [post]
func (a *App) handlePostTOTPConfirm(writer rest.ResponseWriter, r *rest.Request) {
	if !mfaFeatureEnabled() {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return
	}

	account, _, ok := a.mfaCaller(writer, r)
	if !ok {
		return
	}

	payload := &authmodels.TOTPConfirmRequest{}
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

	if settings == nil || settings.TOTP == nil || settings.TOTP.Confirmed {
		utils.RestErrorWrapperUser(writer, "No pending authenticator enrollment", "No pending authenticator enrollment", http.StatusBadRequest)
		return
	}

	secret, err := mfaservice.DecryptSecret(settings.TOTP.SecretEnc)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error reading TOTP secret", http.StatusInternalServerError)
		return
	}

	step, valid := mfaservice.VerifyTOTPCode(secret, payload.Code, time.Now())
	if !valid {
		utils.RestErrorWrapperUser(writer, "Invalid authenticator code", "Invalid authenticator code", http.StatusUnauthorized)
		return
	}

	// keep any recovery codes still valid from another factor; generate the
	// initial set otherwise
	var plainCodes []string
	hashedCodes := settings.RecoveryCodes
	if settings.RecoveryCodesRemaining() == 0 {
		plainCodes, hashedCodes, err = mfaservice.GenerateRecoveryCodes()
		if err != nil {
			utils.RestErrorWrapper(writer, "Error generating recovery codes", http.StatusInternalServerError)
			return
		}
	}

	if err := a.mfaRepo.ConfirmTOTP(ctx, account.Prn, step, hashedCodes); err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.RecoveryCodesResponse{
		RecoveryCodes: plainCodes,
	})
}

// @Summary Remove the TOTP authenticator factor
// @Description Removes the authenticator app factor. Requires the account password (or a fresh session for passwordless accounts). Disables two-factor authentication when no other factor remains.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.MFAPasswordRequest true "Fresh-auth proof"
// @Success 204
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/totp [delete]
func (a *App) handleDeleteTOTP(writer rest.ResponseWriter, r *rest.Request) {
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

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	if settings == nil || settings.TOTP == nil {
		utils.RestErrorWrapperUser(writer, "No authenticator enrolled", "No authenticator enrolled", http.StatusNotFound)
		return
	}

	credCount, err := a.webauthnRepo.CountByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	if err := a.mfaRepo.RemoveTOTP(ctx, account.Prn, credCount > 0); err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// @Summary Regenerate recovery codes
// @Description Replaces the recovery code set with a fresh one (shown exactly once). Requires the account password (or a fresh session for passwordless accounts) and enabled two-factor authentication.
// @Accept json
// @Produce json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.MFAPasswordRequest true "Fresh-auth proof"
// @Success 200 {object} authmodels.RecoveryCodesResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/mfa/recovery/regenerate [post]
func (a *App) handlePostRecoveryRegenerate(writer rest.ResponseWriter, r *rest.Request) {
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

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	if settings == nil || !settings.Enabled {
		utils.RestErrorWrapperUser(writer, "Two-factor authentication is not enabled", "Two-factor authentication is not enabled", http.StatusNotFound)
		return
	}

	plainCodes, hashedCodes, err := mfaservice.GenerateRecoveryCodes()
	if err != nil {
		utils.RestErrorWrapper(writer, "Error generating recovery codes", http.StatusInternalServerError)
		return
	}

	if err := a.mfaRepo.SetRecoveryCodes(ctx, account.Prn, hashedCodes); err != nil {
		utils.RestErrorWrapper(writer, "Error with database connectivity", http.StatusInternalServerError)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.RecoveryCodesResponse{
		RecoveryCodes: plainCodes,
	})
}
