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
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/pkceservice"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// Response from POST /auth/oauth/pkce/init
type CLIInitResponse struct {
	AuthCode     string `json:"auth_code"`
	SessionID    string `json:"session_id"`
	AuthorizeURL string `json:"authorize_url"`
	ExpiresIn    int    `json:"expires_in"`
	Interval     int    `json:"interval"`
}

func (app *App) HandlePostPKCEInit(c *echo.Context) error {
	ctx := c.Request().Context()
	req := struct {
		ClientID            string `json:"client_id"`
		Scope               string `json:"scope"`
		RedirectURI         string `json:"redirect_uri"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		State               string `json:"state"`
	}{}

	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "Invalid request payload", http.StatusBadRequest)
	}

	// Basic validation
	if req.ClientID == "" || req.RedirectURI == "" || req.CodeChallenge == "" || req.CodeChallengeMethod == "" {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "Missing required PKCE parameters", http.StatusBadRequest)
	}

	// Pin the redirect target to the callback URLs registered on client_id
	// before it is persisted into the PKCE state.
	if err := app.validateRedirectURI(ctx, req.ClientID, req.RedirectURI, auditContext(c.Request(), "pkce_init")); err != nil {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", err.Error(), http.StatusBadRequest)
	}

	pks, err := pkceservice.CreatePKCEState(ctx, req.CodeChallenge, req.CodeChallengeMethod, req.RedirectURI, req.State, req.ClientID, req.Scope)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, "internal_error", "Failed to create PKCE state", http.StatusInternalServerError)
	}

	// Public URL (Frontend)
	wwwHost := utils.GetEnv("PANTAHUB_HOST_WWW")
	scheme := utils.GetEnv("PANTAHUB_SCHEME")
	if scheme == "" {
		scheme = "https"
	}

	// If PANTAHUB_HOST_WWW is not set (e.g. localhost), try to fallback or use request host
	if wwwHost == "" {
		wwwHost = c.Request().Host
	}

	authorizeURL := fmt.Sprintf(
		"%s://%s/oauth2/authorize?session_id=%s&response_type=code&client_id=%s&scope=%s",
		scheme,
		wwwHost,
		pks.SessionID,
		url.QueryEscape(req.ClientID),
		url.QueryEscape(req.Scope))

	return echoutil.WriteJSON(c, http.StatusOK, CLIInitResponse{
		AuthCode:     pks.AuthCode,
		SessionID:    pks.SessionID,
		AuthorizeURL: authorizeURL,
		ExpiresIn:    pkceservice.AuthCodeExpiresIn,
		Interval:     pks.Interval,
	})
}

// HandlePostPKCEAuthorize handles the authorization completion from the Web App
func (app *App) HandlePostPKCEAuthorize(c *echo.Context) error {
	ctx := c.Request().Context()

	// Get authenticated user from JWT (required)
	caller := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"].(string)
	if caller == "" {
		return echoutil.RestErrorWrapper(c, "must be authenticated", http.StatusUnauthorized)
	}

	req := struct {
		SessionID string `json:"session_id"` // NEW: Replaces UserCode
	}{}
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	// SCENARIO B: Polling Flow (Session ID provided)
	if req.SessionID != "" {
		pks, found := pkceservice.GetPKCEStateBySessionID(ctx, req.SessionID)
		if !found {
			return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Invalid or expired session", http.StatusBadRequest)
		}

		if pks.IsUsed {
			return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Session already used", http.StatusBadRequest)
		}

		// Link user to PKCE session
		if !pkceservice.UpdatePKCEStateUserID(ctx, pks.AuthCode, caller) {
			return echoutil.RestErrorWrapperUser(c, "internal_error", "Failed to update PKCE state", http.StatusInternalServerError)
		}

		// DO NOT GENERATE TOKEN HERE.
		// Token is generated on-demand when CLI polls with code_verifier.

		return echoutil.WriteJSON(c, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Authorization complete. You can close this window.",
		})
	}

	// SCENARIO A: Callback Flow (Cookies)
	pkceAuthCode := utils.GetCookie(c.Request(), "pkce_auth_code")
	pkceRedirectURI := utils.GetCookie(c.Request(), "pkce_redirect_uri")

	if pkceAuthCode == "" || pkceRedirectURI == "" {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "Missing authorization information (cookies/session_id)", http.StatusBadRequest)
	}

	pks, found := pkceservice.GetPKCEState(ctx, pkceAuthCode)
	if !found {
		return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Invalid or expired session", http.StatusBadRequest)
	}

	// Validate Redirect URI matches cookie
	if pks.RedirectURI != pkceRedirectURI {
		return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Redirect URI mismatch", http.StatusBadRequest)
	}

	if !isValidCallbackURL(pkceRedirectURI) {
		return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Invalid redirect URI", http.StatusBadRequest)
	}

	// Link user to PKCE session
	if !pkceservice.UpdatePKCEStateUserID(ctx, pks.AuthCode, caller) {
		return echoutil.RestErrorWrapperUser(c, "internal_error", "Failed to update PKCE state", http.StatusInternalServerError)
	}

	// Clean up cookies
	utils.DeleteCookie(c.Response(), c.Request(), "pkce_auth_code")
	utils.DeleteCookie(c.Response(), c.Request(), "pkce_redirect_uri")

	// Construct redirect URI with code and state
	params := url.Values{}
	params.Add("code", pks.AuthCode)
	params.Add("state", pks.State)
	redirectURL := pks.RedirectURI + "?" + params.Encode()

	return echoutil.WriteJSON(c, http.StatusOK, map[string]string{
		"code":         pks.AuthCode,
		"redirect_uri": redirectURL,
	})
}

func (app *App) HandlePKCEToken(c *echo.Context) error {
	ctx := c.Request().Context()

	req := struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		AccessCode   string `json:"access-code"`
		CodeVerifier string `json:"code_verifier"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
	}{}

	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "Invalid request payload", http.StatusBadRequest)
	}

	// Normalize code
	code := req.Code
	if code == "" {
		code = req.AccessCode
	}

	// Handle polling grant type
	if req.GrantType == "pkce_poll" {
		pks, found := pkceservice.GetPKCEState(ctx, code)
		if !found {
			return echoutil.RestErrorWrapperUser(c, "expired_token", "The login code has expired", http.StatusBadRequest)
		}

		if time.Since(pks.LastPollAt) < time.Duration(pks.Interval)*time.Second {
			return echoutil.WriteJSON(c, http.StatusBadRequest, map[string]string{
				"error":             "slow_down",
				"error_description": "Polling too frequently",
			})
		}
		pkceservice.UpdateLastPollTime(ctx, code)

		if pks.UserID == "" {
			return echoutil.WriteJSON(c, http.StatusBadRequest, map[string]string{
				"error":             "authorization_pending",
				"error_description": "The user has not yet completed authorization",
			})
		}

		// Verify PKCE code_verifier against stored code_challenge
		// CRITICAL SECURITY CHECK
		switch pks.CodeChallengeMethod {
		case "S256":
			h := sha256.Sum256([]byte(req.CodeVerifier))
			calculatedCodeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
			if calculatedCodeChallenge != pks.CodeChallenge {
				return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Code verifier is invalid", http.StatusBadRequest)
			}
		default:
			return echoutil.RestErrorWrapperUser(c, "invalid_request", "Unsupported code challenge method", http.StatusBadRequest)
		}

		// Generate token on-demand (never pre-stored)
		acc, err := authservices.GetAccount(pks.UserID, app.mongoClient)
		if err != nil {
			return echoutil.RestErrorWrapperUser(c, err.Error(), "Failed to retrieve account information", http.StatusInternalServerError)
		}

		token := jwtgo.New(jwtgo.GetSigningMethod(app.jwtConfig.SigningAlgorithm))
		claims := token.Claims.(jwtgo.MapClaims)

		accPayload := authservices.AccountToPayload(acc)
		for key, value := range accPayload {
			claims[key] = value
		}
		applyPKCEScope(claims, pks.Scope)

		timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 60
		}
		claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()

		if app.jwtConfig.MaxRefresh != 0 {
			claims["orig_iat"] = time.Now().Unix()
		}

		tokenString, err := token.SignedString(app.jwtConfig.Key)
		if err != nil {
			return echoutil.RestErrorWrapperUser(c, err.Error(), "Error signing new token", http.StatusInternalServerError)
		}

		// Mark session as completed (one-time use)
		pkceservice.MarkPKCEStateAsUsed(ctx, code)
		pkceservice.DeletePKCEState(ctx, code)

		return echoutil.WriteJSON(c, http.StatusOK, authmodels.TokenResponse{
			Token:     tokenString,
			TokenType: "bearer",
			ExpiresIn: 3600, // Approximate
		})
	}

	// Validate grant_type
	if req.GrantType != "authorization_code" {
		return echoutil.RestErrorWrapperUser(c, "unsupported_grant_type", "The grant type is not supported", http.StatusBadRequest)
	}

	// Retrieve PKCE state
	pks, found := pkceservice.GetPKCEState(ctx, code)
	if !found {
		return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Authorization code is invalid or expired", http.StatusBadRequest)
	}

	// Check if already used or expired
	if pks.IsUsed || time.Now().After(pks.ExpiresAt) {
		pkceservice.DeletePKCEState(ctx, code) // Clean up
		return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Authorization code already used or expired", http.StatusBadRequest)
	}

	// Validate redirect_uri
	if pks.RedirectURI != req.RedirectURI {
		return echoutil.RestErrorWrapperUser(c, "invalid_redirect_uri", "Provided redirect_uri does not match the one in the authorization request", http.StatusBadRequest)
	}

	// Validate code_verifier
	switch pks.CodeChallengeMethod {
	case "S256":
		h := sha256.Sum256([]byte(req.CodeVerifier))
		calculatedCodeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
		if calculatedCodeChallenge != pks.CodeChallenge {
			return echoutil.RestErrorWrapperUser(c, "invalid_grant", "Code verifier is invalid", http.StatusBadRequest)
		}
	default:
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "Unsupported code challenge method", http.StatusBadRequest)
	}

	// Mark PKCE state as used
	pkceservice.MarkPKCEStateAsUsed(ctx, code)

	acc, err := authservices.GetAccount(pks.UserID, app.mongoClient)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "Failed to retrieve account information", http.StatusInternalServerError)
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwtConfig.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	accPayload := authservices.AccountToPayload(acc)
	for key, value := range accPayload {
		claims[key] = value
	}
	applyPKCEScope(claims, pks.Scope)

	timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		timeout = 60
	}
	claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()

	if app.jwtConfig.MaxRefresh != 0 {
		claims["orig_iat"] = time.Now().Unix()
	}

	tokenString, err := token.SignedString(app.jwtConfig.Key)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "Error signing new token", http.StatusInternalServerError)
	}

	// Delete PKCE state after successful token issuance
	pkceservice.DeletePKCEState(ctx, code)

	return echoutil.WriteJSON(c, http.StatusOK, authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
	})
}

// HandlePKCEAuthorize handles the authorization request for PKCE flow
func (app *App) HandlePKCEAuthorize(c *echo.Context) error {
	ctx := c.Request().Context()
	queryParams := c.Request().URL.Query()

	// SCENARIO B: Unified/Polling Flow (session_id provided)
	// The CLI called /pkce/init, got a URL with session_id, and the user opened it.
	if sessionID := queryParams.Get("session_id"); sessionID != "" {
		pks, found := pkceservice.GetPKCEStateBySessionID(ctx, sessionID)
		if !found {
			return echoutil.RestErrorWrapperUser(c, "invalid_request", "Invalid or expired session_id", http.StatusBadRequest)
		}

		wwwHost := utils.GetEnv("PANTAHUB_HOST_WWW")
		scheme := utils.GetEnv("PANTAHUB_SCHEME")

		// Redirect to Hub with session_id. The Hub will call POST /oauth/authorize with this session_id.
		redirectURL := fmt.Sprintf(
			"%s://%s/oauth2/authorize?session_id=%s&client_id=%s&scope=%s&response_type=code",
			scheme,
			wwwHost,
			pks.SessionID,
			pks.ClientID,
			pks.Scope,
		)
		http.Redirect(c.Response(), c.Request(), redirectURL, http.StatusTemporaryRedirect)
		return nil
	}

	// SCENARIO A: Legacy/Callback Flow (No session_id)
	// The CLI/App constructed the URL manually and opened it.
	clientID := queryParams.Get("client_id")
	redirectURI := queryParams.Get("redirect_uri")
	codeChallenge := queryParams.Get("code_challenge")
	codeChallengeMethod := queryParams.Get("code_challenge_method")
	scope := queryParams.Get("scope")
	state := queryParams.Get("state")
	responseType := queryParams.Get("response_type")

	if responseType == "" {
		responseType = "code"
	}

	// Basic validation
	if clientID == "" || redirectURI == "" || codeChallenge == "" || codeChallengeMethod == "" {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "Missing required PKCE parameters", http.StatusBadRequest)
	}

	// Pin the redirect target to the callback URLs registered on client_id
	// before it is persisted into the PKCE state or echoed into the authorize
	// URL handed to the web app.
	if err := app.validateRedirectURI(ctx, clientID, redirectURI, auditContext(c.Request(), "pkce_authorize")); err != nil {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", err.Error(), http.StatusBadRequest)
	}

	// Store the PKCE state
	pks, err := pkceservice.CreatePKCEState(ctx, codeChallenge, codeChallengeMethod, redirectURI, state, clientID, scope)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, "internal_error", "Failed to create PKCE state", http.StatusInternalServerError)
	}

	cookieExpires := pks.ExpiresAt
	utils.SetCookie(c.Response(), c.Request(), "pkce_auth_code", pks.AuthCode, utils.WithExpires(cookieExpires))
	utils.SetCookie(c.Response(), c.Request(), "pkce_redirect_uri", pks.RedirectURI, utils.WithExpires(cookieExpires))

	wwwHost := utils.GetEnv("PANTAHUB_HOST_WWW")
	scheme := utils.GetEnv("PANTAHUB_SCHEME")
	url := fmt.Sprintf(
		"%s://%s/oauth2/authorize?client_id=%s&auth_code=%s&redirect_uri=%s&state=%s&scope=%s&response_type=%s",
		scheme,
		wwwHost,
		clientID,
		pks.AuthCode,
		url.QueryEscape(redirectURI),
		state,
		scope,
		responseType,
	)

	http.Redirect(c.Response(), c.Request(), url, http.StatusTemporaryRedirect)
	return nil
}

// applyPKCEScope narrows the minted token to the scope the user consented to
// on the authorize page, exactly as POST /auth/login narrows a password
// session to its requested scope. An empty scope keeps the account default.
func applyPKCEScope(claims jwtgo.MapClaims, requested string) {
	scopes := utils.ScopeStringFilterBy(strings.Fields(requested), "", "")
	if len(scopes) > 0 {
		claims["scopes"] = strings.Join(scopes, " ")
	}
}
