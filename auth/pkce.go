// Copyright 2016-2025  Pantacor Ltd.
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
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/pkceservice"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Response from POST /auth/oauth/pkce/init
type CLIInitResponse struct {
	AuthCode     string `json:"auth_code"`
	SessionID    string `json:"session_id"`
	AuthorizeURL string `json:"authorize_url"`
	ExpiresIn    int    `json:"expires_in"`
	Interval     int    `json:"interval"`
}

func (app *App) HandlePostPKCEInit(w rest.ResponseWriter, r *rest.Request) {
	ctx := r.Request.Context()
	req := struct {
		ClientID            string `json:"client_id"`
		Scope               string `json:"scope"`
		RedirectURI         string `json:"redirect_uri"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		State               string `json:"state"`
	}{}

	if err := r.DecodeJsonPayload(&req); err != nil {
		utils.RestErrorWrapperUser(w, "invalid_request", "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Basic validation
	if req.ClientID == "" || req.RedirectURI == "" || req.CodeChallenge == "" || req.CodeChallengeMethod == "" {
		utils.RestErrorWrapperUser(w, "invalid_request", "Missing required PKCE parameters", http.StatusBadRequest)
		return
	}

	pks, err := pkceservice.CreatePKCEState(ctx, req.CodeChallenge, req.CodeChallengeMethod, req.RedirectURI, req.State, req.ClientID, req.Scope)
	if err != nil {
		utils.RestErrorWrapperUser(w, "internal_error", "Failed to create PKCE state", http.StatusInternalServerError)
		return
	}

	// Public URL (Frontend)
	wwwHost := utils.GetEnv("PANTAHUB_HOST_WWW")
	scheme := utils.GetEnv("PANTAHUB_SCHEME")
	if scheme == "" {
		scheme = "https"
	}

	// If PANTAHUB_HOST_WWW is not set (e.g. localhost), try to fallback or use request host
	if wwwHost == "" {
		wwwHost = r.Host
	}

	authorizeURL := fmt.Sprintf(
		"%s://%s/oauth2/authorize?session_id=%s&response_type=code&client_id=%s&scope=%s",
		scheme,
		wwwHost,
		pks.SessionID,
		url.QueryEscape(req.ClientID),
		url.QueryEscape(req.Scope))

	w.WriteJson(CLIInitResponse{
		AuthCode:     pks.AuthCode,
		SessionID:    pks.SessionID,
		AuthorizeURL: authorizeURL,
		ExpiresIn:    pkceservice.AuthCodeExpiresIn,
		Interval:     pks.Interval,
	})
}

// HandlePostPKCEAuthorize handles the authorization completion from the Web App
func (app *App) HandlePostPKCEAuthorize(w rest.ResponseWriter, r *rest.Request) {
	ctx := r.Request.Context()

	// Get authenticated user from JWT (required)
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)
	if caller == "" {
		utils.RestErrorWrapper(w, "must be authenticated", http.StatusUnauthorized)
		return
	}

	req := struct {
		SessionID string `json:"session_id"` // NEW: Replaces UserCode
	}{}
	r.DecodeJsonPayload(&req)

	// SCENARIO B: Polling Flow (Session ID provided)
	if req.SessionID != "" {
		pks, found := pkceservice.GetPKCEStateBySessionID(ctx, req.SessionID)
		if !found {
			utils.RestErrorWrapperUser(w, "invalid_grant", "Invalid or expired session", http.StatusBadRequest)
			return
		}

		if pks.IsUsed {
			utils.RestErrorWrapperUser(w, "invalid_grant", "Session already used", http.StatusBadRequest)
			return
		}

		// Link user to PKCE session
		if !pkceservice.UpdatePKCEStateUserID(ctx, pks.AuthCode, caller) {
			utils.RestErrorWrapperUser(w, "internal_error", "Failed to update PKCE state", http.StatusInternalServerError)
			return
		}

		// DO NOT GENERATE TOKEN HERE.
		// Token is generated on-demand when CLI polls with code_verifier.

		w.WriteJson(map[string]interface{}{
			"success": true,
			"message": "Authorization complete. You can close this window.",
		})
		return
	}

	// SCENARIO A: Callback Flow (Cookies)
	pkceAuthCode := utils.GetCookie(r, "pkce_auth_code")
	pkceRedirectURI := utils.GetCookie(r, "pkce_redirect_uri")

	if pkceAuthCode == "" || pkceRedirectURI == "" {
		utils.RestErrorWrapperUser(w, "invalid_request", "Missing authorization information (cookies/session_id)", http.StatusBadRequest)
		return
	}

	pks, found := pkceservice.GetPKCEState(ctx, pkceAuthCode)
	if !found {
		utils.RestErrorWrapperUser(w, "invalid_grant", "Invalid or expired session", http.StatusBadRequest)
		return
	}

	// Validate Redirect URI matches cookie
	if pks.RedirectURI != pkceRedirectURI {
		utils.RestErrorWrapperUser(w, "invalid_grant", "Redirect URI mismatch", http.StatusBadRequest)
		return
	}

	if !isValidCallbackURL(pkceRedirectURI) {
		utils.RestErrorWrapperUser(w, "invalid_grant", "Invalid redirect URI", http.StatusBadRequest)
		return
	}

	// Link user to PKCE session
	if !pkceservice.UpdatePKCEStateUserID(ctx, pks.AuthCode, caller) {
		utils.RestErrorWrapperUser(w, "internal_error", "Failed to update PKCE state", http.StatusInternalServerError)
		return
	}

	// Clean up cookies
	utils.DeleteCookie(w, r, "pkce_auth_code")
	utils.DeleteCookie(w, r, "pkce_redirect_uri")

	// Construct redirect URI with code and state
	params := url.Values{}
	params.Add("code", pks.AuthCode)
	params.Add("state", pks.State)
	redirectURL := pks.RedirectURI + "?" + params.Encode()

	w.WriteJson(map[string]string{
		"code":         pks.AuthCode,
		"redirect_uri": redirectURL,
	})
}

func (app *App) HandlePKCEToken(w rest.ResponseWriter, r *rest.Request) {
	ctx := r.Request.Context()

	req := struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		AccessCode   string `json:"access-code"`
		CodeVerifier string `json:"code_verifier"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
	}{}

	if err := r.DecodeJsonPayload(&req); err != nil {
		utils.RestErrorWrapperUser(w, "invalid_request", "Invalid request payload", http.StatusBadRequest)
		return
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
			utils.RestErrorWrapperUser(w, "expired_token", "The login code has expired", http.StatusBadRequest)
			return
		}

		if time.Since(pks.LastPollAt) < time.Duration(pks.Interval)*time.Second {
			w.WriteHeader(http.StatusBadRequest)
			w.WriteJson(map[string]string{
				"error":             "slow_down",
				"error_description": "Polling too frequently",
			})
			return
		}
		pkceservice.UpdateLastPollTime(ctx, code)

		if pks.UserID == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.WriteJson(map[string]string{
				"error":             "authorization_pending",
				"error_description": "The user has not yet completed authorization",
			})
			return
		}

		// Verify PKCE code_verifier against stored code_challenge
		// CRITICAL SECURITY CHECK
		switch pks.CodeChallengeMethod {
		case "S256":
			h := sha256.Sum256([]byte(req.CodeVerifier))
			calculatedCodeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
			if calculatedCodeChallenge != pks.CodeChallenge {
				utils.RestErrorWrapperUser(w, "invalid_grant", "Code verifier is invalid", http.StatusBadRequest)
				return
			}
		default:
			utils.RestErrorWrapperUser(w, "invalid_request", "Unsupported code challenge method", http.StatusBadRequest)
			return
		}

		// Generate token on-demand (never pre-stored)
		acc, err := authservices.GetAccount(pks.UserID, app.mongoClient)
		if err != nil {
			utils.RestErrorWrapperUser(w, err.Error(), "Failed to retrieve account information", http.StatusInternalServerError)
			return
		}

		token := jwtgo.New(jwtgo.GetSigningMethod(app.jwtMiddleware.SigningAlgorithm))
		claims := token.Claims.(jwtgo.MapClaims)

		accPayload := authservices.AccountToPayload(acc)
		for key, value := range accPayload {
			claims[key] = value
		}

		timeoutStr := utils.GetEnv(utils.EnvAnonJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 5
		}
		claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()

		if app.jwtMiddleware.MaxRefresh != 0 {
			claims["orig_iat"] = time.Now().Unix()
		}

		tokenString, err := token.SignedString(app.jwtMiddleware.Key)
		if err != nil {
			utils.RestErrorWrapperUser(w, err.Error(), "Error signing new token", http.StatusInternalServerError)
			return
		}

		// Mark session as completed (one-time use)
		pkceservice.MarkPKCEStateAsUsed(ctx, code)
		pkceservice.DeletePKCEState(ctx, code)

		w.WriteJson(authmodels.TokenResponse{
			Token:     tokenString,
			TokenType: "bearer",
			ExpiresIn: 3600, // Approximate
		})
		return
	}

	// Validate grant_type
	if req.GrantType != "authorization_code" {
		utils.RestErrorWrapperUser(w, "unsupported_grant_type", "The grant type is not supported", http.StatusBadRequest)
		return
	}

	// Retrieve PKCE state
	pks, found := pkceservice.GetPKCEState(ctx, code)
	if !found {
		utils.RestErrorWrapperUser(w, "invalid_grant", "Authorization code is invalid or expired", http.StatusBadRequest)
		return
	}

	// Check if already used or expired
	if pks.IsUsed || time.Now().After(pks.ExpiresAt) {
		pkceservice.DeletePKCEState(ctx, code) // Clean up
		utils.RestErrorWrapperUser(w, "invalid_grant", "Authorization code already used or expired", http.StatusBadRequest)
		return
	}

	// Validate redirect_uri
	if pks.RedirectURI != req.RedirectURI {
		utils.RestErrorWrapperUser(w, "invalid_redirect_uri", "Provided redirect_uri does not match the one in the authorization request", http.StatusBadRequest)
		return
	}

	// Validate code_verifier
	switch pks.CodeChallengeMethod {
	case "S256":
		h := sha256.Sum256([]byte(req.CodeVerifier))
		calculatedCodeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
		if calculatedCodeChallenge != pks.CodeChallenge {
			utils.RestErrorWrapperUser(w, "invalid_grant", "Code verifier is invalid", http.StatusBadRequest)
			return
		}
	default:
		utils.RestErrorWrapperUser(w, "invalid_request", "Unsupported code challenge method", http.StatusBadRequest)
		return
	}

	// Mark PKCE state as used
	pkceservice.MarkPKCEStateAsUsed(ctx, code)

	acc, err := authservices.GetAccount(pks.UserID, app.mongoClient)
	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "Failed to retrieve account information", http.StatusInternalServerError)
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwtMiddleware.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	accPayload := authservices.AccountToPayload(acc)
	for key, value := range accPayload {
		claims[key] = value
	}

	timeoutStr := utils.GetEnv(utils.EnvAnonJWTTimeoutMinutes)
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		timeout = 5
	}
	claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()

	if app.jwtMiddleware.MaxRefresh != 0 {
		claims["orig_iat"] = time.Now().Unix()
	}

	tokenString, err := token.SignedString(app.jwtMiddleware.Key)
	if err != nil {
		utils.RestErrorWrapperUser(w, err.Error(), "Error signing new token", http.StatusInternalServerError)
		return
	}

	// Delete PKCE state after successful token issuance
	pkceservice.DeletePKCEState(ctx, code)

	w.WriteJson(authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
	})
}

// HandlePKCEAuthorize handles the authorization request for PKCE flow
func (app *App) HandlePKCEAuthorize(w rest.ResponseWriter, r *rest.Request) {
	ctx := r.Request.Context()
	queryParams := r.URL.Query()

	// SCENARIO B: Unified/Polling Flow (session_id provided)
	// The CLI called /pkce/init, got a URL with session_id, and the user opened it.
	if sessionID := queryParams.Get("session_id"); sessionID != "" {
		pks, found := pkceservice.GetPKCEStateBySessionID(ctx, sessionID)
		if !found {
			utils.RestErrorWrapperUser(w, "invalid_request", "Invalid or expired session_id", http.StatusBadRequest)
			return
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
		http.Redirect(w, r.Request, redirectURL, http.StatusTemporaryRedirect)
		return
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
		utils.RestErrorWrapperUser(w, "invalid_request", "Missing required PKCE parameters", http.StatusBadRequest)
		return
	}

	// Store the PKCE state
	pks, err := pkceservice.CreatePKCEState(ctx, codeChallenge, codeChallengeMethod, redirectURI, state, clientID, scope)
	if err != nil {
		utils.RestErrorWrapperUser(w, "internal_error", "Failed to create PKCE state", http.StatusInternalServerError)
		return
	}

	cookieExpires := pks.ExpiresAt
	utils.SetCookie(w, r, "pkce_auth_code", pks.AuthCode, utils.WithExpires(cookieExpires))
	utils.SetCookie(w, r, "pkce_redirect_uri", pks.RedirectURI, utils.WithExpires(cookieExpires))

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

	http.Redirect(w, r.Request, url, http.StatusTemporaryRedirect)
}
