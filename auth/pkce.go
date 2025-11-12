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

func (app *App) HandlePKCEToken(w rest.ResponseWriter, r *rest.Request) {
	ctx := r.Request.Context()
	req := new(authmodels.TokenRequest)
	if err := r.DecodeJsonPayload(&req); err != nil {
		utils.RestErrorWrapperUser(w, "invalid_request", "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Validate grant_type
	if req.GrantType != "authorization_code" {
		utils.RestErrorWrapperUser(w, "unsupported_grant_type", "The grant type is not supported", http.StatusBadRequest)
		return
	}

	// Retrieve PKCE state
	pks, found := pkceservice.GetPKCEState(ctx, req.Code)
	if !found {
		utils.RestErrorWrapperUser(w, "invalid_grant", "Authorization code is invalid or expired", http.StatusBadRequest)
		return
	}

	// Check if already used or expired
	if pks.IsUsed || time.Now().After(pks.ExpiresAt) {
		pkceservice.DeletePKCEState(ctx, req.Code) // Clean up
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
	pkceservice.MarkPKCEStateAsUsed(ctx, req.Code)

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
	pkceservice.DeletePKCEState(ctx, req.Code)

	w.WriteJson(authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
	})
}

// HandlePKCEAuthorize handles the authorization request for PKCE flow
func (app *App) HandlePKCEAuthorize(w rest.ResponseWriter, r *rest.Request) {
	ctx := r.Request.Context()
	queryParams := r.URL.Query()

	clientID := queryParams.Get("client_id")
	redirectURI := queryParams.Get("redirect_uri")
	codeChallenge := queryParams.Get("code_challenge")
	codeChallengeMethod := queryParams.Get("code_challenge_method")
	scope := queryParams.Get("scope")
	state := queryParams.Get("state")
	resposeType := queryParams.Get("response_type")

	if resposeType == "" {
		resposeType = "code"
	}

	// Basic validation
	if clientID == "" || redirectURI == "" || codeChallenge == "" || codeChallengeMethod == "" {
		utils.RestErrorWrapperUser(w, "invalid_request", "Missing required PKCE parameters", http.StatusBadRequest)
		return
	}

	// Store the PKCE state
	pks, err := pkceservice.CreatePKCEState(ctx, codeChallenge, codeChallengeMethod, redirectURI, state)
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
		resposeType,
	)

	http.Redirect(w, r.Request, url, http.StatusTemporaryRedirect)
}
