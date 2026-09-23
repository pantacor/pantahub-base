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
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// maybeStartMFALogin decides whether a password login must step up to a
// second factor. Returns handled == true when it already wrote a response
// (MFA challenge or auth failure); handled == false means the caller should
// proceed with the normal single-step login.
//
// Personal access tokens presented as the password are the machine channel
// and stay exempt from the step-up (like the device x509 and session paths).
func (a *App) maybeStartMFALogin(c *echo.Context, payload *authmodels.LoginRequestPayload) (handled bool) {
	if !mfaFeatureEnabled() || a.mfaRepo == nil {
		return false
	}

	// call-as syntax: MFA applies to the authenticating (left) user
	loginUser := strings.SplitN(payload.Username, "==>", 2)[0]

	account, err := authservices.GetAccount(loginUser, a.mongoClient)
	if err != nil {
		// unknown account or non-account principal: normal path decides
		return false
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		// fail closed: if we cannot read the MFA state we must NOT fall
		// through to a single-factor login for an account that may be
		// MFA-protected. A DB blip must never strip the second factor.
		_ = echoutil.RestErrorWrapperUser(c, "Error with database connectivity", "Please try again later", http.StatusServiceUnavailable)
		return true
	}
	if settings == nil || !settings.Enabled {
		return false
	}

	// a valid personal access token bypasses the step-up
	if authservices.IsValidPersonalToken(ctx, loginUser, account.Prn, payload.Password, a.mongoClient) {
		return false
	}

	// MFA account with a password credential: verify it ourselves, then
	// hand out the pending token instead of a session
	if !a.jwtConfig.Authenticator(payload.Username, payload.Password) {
		_ = echoutil.RestErrorWrite(c, &utils.RError{
			Msg:   "Authentication Failed",
			Error: "Authentication Failed",
			Code:  http.StatusUnauthorized,
		})
		return true
	}

	methods := a.availableMFAMethods(ctx, settings)

	mfaToken, err := mfaservice.CreateMFAPendingToken(
		a.jwtConfig,
		payload.Username,
		account.Prn,
		payload.Scope,
		[]string{"pwd"},
		methods,
	)
	if err != nil {
		_ = echoutil.RestErrorWrapper(c, "Error creating MFA token", http.StatusInternalServerError)
		return true
	}

	noStore(c)
	_ = echoutil.WriteJSON(c, http.StatusOK, authmodels.MFARequiredResponse{
		MFARequired: true,
		MFAToken:    mfaToken,
		Methods:     methods,
	})
	return true
}

// availableMFAMethods lists the second factors the account can complete a
// pending login with
func (a *App) availableMFAMethods(ctx context.Context, settings *storage.MFASettings) []string {
	methods := []string{}
	if settings.HasConfirmedTOTP() {
		methods = append(methods, authmodels.MFAMethodTOTP)
	}
	if a.webauthnRepo != nil {
		if count, err := a.webauthnRepo.CountByOwner(ctx, settings.Owner); err == nil && count > 0 {
			methods = append(methods, authmodels.MFAMethodWebauthn)
		}
	}
	if settings.RecoveryCodesRemaining() > 0 {
		methods = append(methods, authmodels.MFAMethodRecovery)
	}
	return methods
}

// mfaPendingFromRequest validates the pending token and loads the matching
// MFA settings; writes the (deliberately generic) error responses itself.
func (a *App) mfaPendingFromRequest(c *echo.Context, mfaToken string) (*mfaservice.MFAPendingClaims, *storage.MFASettings, bool) {
	userAgent := c.Request().Header.Get("User-Agent")
	if userAgent == "" {
		_ = echoutil.RestErrorWrapperUser(c, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return nil, nil, false
	}

	if !mfaFeatureEnabled() || a.mfaRepo == nil {
		_ = echoutil.RestErrorWrapperUser(c, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return nil, nil, false
	}

	claims, err := mfaservice.ParseMFAPendingToken(a.jwtConfig, mfaToken)
	if err != nil {
		_ = echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return nil, nil, false
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, claims.Prn)
	if err != nil || settings == nil || !settings.Enabled {
		_ = echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return nil, nil, false
	}

	if settings.IsLocked(time.Now()) {
		_ = echoutil.RestErrorWrapperUser(c, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
		return nil, nil, false
	}

	return claims, settings, true
}

// mfaLoginFailure counts a failed proof and answers with a generic 401 (or
// 429 when the failure crossed the lockout threshold)
func (a *App) mfaLoginFailure(c *echo.Context, ownerPrn string) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	locked, err := a.mfaRepo.RegisterFailure(ctx, ownerPrn, mfaservice.MaxMFAFailures, mfaservice.MFALockDuration)
	if err == nil && locked {
		return echoutil.RestErrorWrapperUser(c, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
	}

	return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
}

// mfaLoginSuccess consumes the single-use pending token and mints the full
// session token with the authentication-methods (amr) trail
func (a *App) mfaLoginSuccess(c *echo.Context, claims *mfaservice.MFAPendingClaims, factor string) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	payload := &authmodels.LoginRequestPayload{
		Username: claims.Username,
		Scope:    claims.Scope,
	}

	extraClaims := map[string]interface{}{
		"amr":       append(append([]string{}, claims.Amr...), factor),
		"auth_time": time.Now().Unix(),
	}

	// Mint the session first, then burn the single-use pending token. If
	// minting fails the challenge is NOT consumed, so a transient error lets
	// the user retry instead of stranding a valid, already-proven login.
	// Single-use is still enforced: the token is only released after
	// ConsumeJTI succeeds (its unique insert rejects a replayed/raced jti).
	tokenString, rerr := authservices.MintAuthenticatedUserToken(payload, extraClaims, a.jwtConfig, a.mongoClient)
	if rerr != nil {
		return echoutil.RestErrorWrite(c, rerr)
	}

	if err := a.mfaRepo.ConsumeJTI(ctx, claims.ID, claims.ExpiresAt.Time); err != nil {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	noStore(c)
	return echoutil.WriteJSON(c, http.StatusOK, authmodels.TokenResponse{
		Token: tokenString,
	})
}

// @Summary Complete a pending login with an authenticator (TOTP) code
// @Description Second step of a two-factor login: exchanges the mfa_token from POST /auth/login plus a valid authenticator code for a session token.
// @Accept json
// @Produce json
// @Tags auth
// @Param body body authmodels.MFALoginRequest true "Pending token and authenticator code"
// @Success 200 {object} authmodels.TokenResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/login/mfa/totp [post]
func (a *App) handlePostLoginMFATOTP(c *echo.Context) error {
	payload := &authmodels.MFALoginRequest{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode request", http.StatusBadRequest)
	}

	claims, settings, ok := a.mfaPendingFromRequest(c, payload.MFAToken)
	if !ok {
		return nil
	}

	if !claims.HasMethod(authmodels.MFAMethodTOTP) || !settings.HasConfirmedTOTP() {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	secret, err := mfaservice.DecryptSecret(settings.TOTP.SecretEnc)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error reading TOTP secret", http.StatusInternalServerError)
	}

	step, valid := mfaservice.VerifyTOTPCode(secret, payload.Code, time.Now())
	if !valid {
		return a.mfaLoginFailure(c, claims.Prn)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// atomic: rejects codes from an already-consumed time step (replay)
	if err := a.mfaRepo.UseTOTPStep(ctx, claims.Prn, step); err != nil {
		return a.mfaLoginFailure(c, claims.Prn)
	}

	return a.mfaLoginSuccess(c, claims, "otp")
}

// @Summary Complete a pending login with a recovery code
// @Description Second step of a two-factor login using a single-use recovery code. The code is invalidated on use.
// @Accept json
// @Produce json
// @Tags auth
// @Param body body authmodels.MFALoginRequest true "Pending token and recovery code"
// @Success 200 {object} authmodels.TokenResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/login/mfa/recovery [post]
func (a *App) handlePostLoginMFARecovery(c *echo.Context) error {
	payload := &authmodels.MFALoginRequest{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode request", http.StatusBadRequest)
	}

	claims, settings, ok := a.mfaPendingFromRequest(c, payload.MFAToken)
	if !ok {
		return nil
	}

	if !claims.HasMethod(authmodels.MFAMethodRecovery) {
		return echoutil.RestErrorWrapperUser(c, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
	}

	index, valid := mfaservice.VerifyRecoveryCode(settings.RecoveryCodes, payload.Code)
	if !valid {
		return a.mfaLoginFailure(c, claims.Prn)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// atomic: a code can only ever be consumed once
	if err := a.mfaRepo.UseRecoveryCode(ctx, claims.Prn, index); err != nil {
		return a.mfaLoginFailure(c, claims.Prn)
	}

	return a.mfaLoginSuccess(c, claims, "recovery")
}
