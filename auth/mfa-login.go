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
	"net/http"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// maybeStartMFALogin decides whether a password login must step up to a
// second factor. Returns handled == true when it already wrote a response
// (MFA challenge or auth failure); handled == false means the caller should
// proceed with the normal single-step login.
//
// Personal access tokens presented as the password are the machine channel
// and stay exempt from the step-up (like the device x509 and session paths).
func (a *App) maybeStartMFALogin(writer rest.ResponseWriter, r *rest.Request, payload *authmodels.LoginRequestPayload) (handled bool) {
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

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		// fail closed: if we cannot read the MFA state we must NOT fall
		// through to a single-factor login for an account that may be
		// MFA-protected. A DB blip must never strip the second factor.
		utils.RestErrorWrapperUser(writer, "Error with database connectivity", "Please try again later", http.StatusServiceUnavailable)
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
	if !a.jwtMiddleware.Authenticator(payload.Username, payload.Password) {
		utils.RestErrorWrite(writer, &utils.RError{
			Msg:   "Authentication Failed",
			Error: "Authentication Failed",
			Code:  http.StatusUnauthorized,
		})
		return true
	}

	methods := a.availableMFAMethods(ctx, settings)

	mfaToken, err := mfaservice.CreateMFAPendingToken(
		a.jwtMiddleware,
		payload.Username,
		account.Prn,
		payload.Scope,
		[]string{"pwd"},
		methods,
	)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error creating MFA token", http.StatusInternalServerError)
		return true
	}

	noStore(writer)
	writer.WriteJson(authmodels.MFARequiredResponse{
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
func (a *App) mfaPendingFromRequest(writer rest.ResponseWriter, r *rest.Request, mfaToken string) (*mfaservice.MFAPendingClaims, *storage.MFASettings, bool) {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return nil, nil, false
	}

	if !mfaFeatureEnabled() || a.mfaRepo == nil {
		utils.RestErrorWrapperUser(writer, "MFA is not enabled on this server", "MFA is not enabled on this server", http.StatusNotImplemented)
		return nil, nil, false
	}

	claims, err := mfaservice.ParseMFAPendingToken(a.jwtMiddleware, mfaToken)
	if err != nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return nil, nil, false
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, claims.Prn)
	if err != nil || settings == nil || !settings.Enabled {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return nil, nil, false
	}

	if settings.IsLocked(time.Now()) {
		utils.RestErrorWrapperUser(writer, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
		return nil, nil, false
	}

	return claims, settings, true
}

// mfaLoginFailure counts a failed proof and answers with a generic 401 (or
// 429 when the failure crossed the lockout threshold)
func (a *App) mfaLoginFailure(writer rest.ResponseWriter, r *rest.Request, ownerPrn string) {
	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	locked, err := a.mfaRepo.RegisterFailure(ctx, ownerPrn, mfaservice.MaxMFAFailures, mfaservice.MFALockDuration)
	if err == nil && locked {
		utils.RestErrorWrapperUser(writer, "Too many attempts; try again later", "Too many attempts; try again later", http.StatusTooManyRequests)
		return
	}

	utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
}

// mfaLoginSuccess consumes the single-use pending token and mints the full
// session token with the authentication-methods (amr) trail
func (a *App) mfaLoginSuccess(writer rest.ResponseWriter, r *rest.Request, claims *mfaservice.MFAPendingClaims, factor string) {
	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
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
	tokenString, rerr := authservices.MintAuthenticatedUserToken(payload, extraClaims, a.jwtMiddleware, a.mongoClient)
	if rerr != nil {
		utils.RestErrorWrite(writer, rerr)
		return
	}

	if err := a.mfaRepo.ConsumeJTI(ctx, claims.Id, time.Unix(claims.ExpiresAt, 0)); err != nil {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	noStore(writer)
	writer.WriteJson(authmodels.TokenResponse{
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
func (a *App) handlePostLoginMFATOTP(writer rest.ResponseWriter, r *rest.Request) {
	payload := &authmodels.MFALoginRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	claims, settings, ok := a.mfaPendingFromRequest(writer, r, payload.MFAToken)
	if !ok {
		return
	}

	if !claims.HasMethod(authmodels.MFAMethodTOTP) || !settings.HasConfirmedTOTP() {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	secret, err := mfaservice.DecryptSecret(settings.TOTP.SecretEnc)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error reading TOTP secret", http.StatusInternalServerError)
		return
	}

	step, valid := mfaservice.VerifyTOTPCode(secret, payload.Code, time.Now())
	if !valid {
		a.mfaLoginFailure(writer, r, claims.Prn)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	// atomic: rejects codes from an already-consumed time step (replay)
	if err := a.mfaRepo.UseTOTPStep(ctx, claims.Prn, step); err != nil {
		a.mfaLoginFailure(writer, r, claims.Prn)
		return
	}

	a.mfaLoginSuccess(writer, r, claims, "otp")
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
func (a *App) handlePostLoginMFARecovery(writer rest.ResponseWriter, r *rest.Request) {
	payload := &authmodels.MFALoginRequest{}
	if err := r.DecodeJsonPayload(payload); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode request", http.StatusBadRequest)
		return
	}

	claims, settings, ok := a.mfaPendingFromRequest(writer, r, payload.MFAToken)
	if !ok {
		return
	}

	if !claims.HasMethod(authmodels.MFAMethodRecovery) {
		utils.RestErrorWrapperUser(writer, "Authentication Failed", "Authentication Failed", http.StatusUnauthorized)
		return
	}

	index, valid := mfaservice.VerifyRecoveryCode(settings.RecoveryCodes, payload.Code)
	if !valid {
		a.mfaLoginFailure(writer, r, claims.Prn)
		return
	}

	ctx, cancel := context.WithTimeout(r.Request.Context(), 10*time.Second)
	defer cancel()

	// atomic: a code can only ever be consumed once
	if err := a.mfaRepo.UseRecoveryCode(ctx, claims.Prn, index); err != nil {
		a.mfaLoginFailure(writer, r, claims.Prn)
		return
	}

	a.mfaLoginSuccess(writer, r, claims, "recovery")
}
