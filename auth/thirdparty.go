// Copyright 2016-2020  Pantacor Ltd.
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
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/oauth"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

const dubKeyErrCode = 11000

// TokenPayload login token payload
type TokenPayload struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	Scopes    string `json:"scopes"`
}

// HandlethirdPartyLogin login or register user using thirdparty integration
// @Summary login or register user using thirdparty integration
// @Description login or register user using thirdparty integration
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param service path string false "External oAuth service"
// @Param returnto query string false "Return to with implicit token"
// @Redirect 303
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 403 {object} utils.RError "user has no admin role"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/thirdparty/{service}/login [get]
func (a *App) HandlethirdPartyLogin(w rest.ResponseWriter, r *rest.Request) {
	oauth.AuthorizeByService(w, r)
}

// HandlethirdPartyCallback login or register user using thirdparty integration
// @Summary login or register user using thirdparty integration
// @Description login or register user using thirdparty integration
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param service path string false "External oAuth service"
// @Param returnto query string false "Return to with implicit token"
// @Success 200 {object} TokenPayload
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 403 {object} utils.RError "user has no admin role"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/thirdparty/{service}/callback [get]
func (a *App) HandlethirdPartyCallback(w rest.ResponseWriter, r *rest.Request) {
	payload, err := oauth.CbByService(r)
	if err != nil {
		utils.RestError(w, err, "Unable to connect to thirdparty service", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	account, err := getUserByEmail(payload.Email, collection)
	if err != nil && err != mongo.ErrNoDocuments {
		utils.RestError(w, err, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	if err == mongo.ErrNoDocuments {
		account, err = createUser(payload.Email, payload.Nick, "", "", collection)
		if err != nil && isDubplicateKey("nick", err) {
			scopeNick := payload.Nick + "_" + string(payload.Service)
			account, err = createUser(payload.Email, scopeNick, "", "", collection)
		}
	}
	if err != nil {
		utils.RestError(w, err, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	token, err := createAccountToken(account)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		return
	}

	if payload.RedirectTo != "" {
		redirectURI := fmt.Sprintf("%s#token=%s", payload.RedirectTo, url.QueryEscape(token.Token))
		http.Redirect(w, r.Request, redirectURI, http.StatusTemporaryRedirect)
	}

	w.WriteJson(token)
}

func createAccountToken(account *accounts.Account) (*TokenPayload, error) {
	token := jwt.New(jwt.GetSigningMethod("RS256"))
	claims := token.Claims.(jwt.MapClaims)

	timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		return nil, err
	}
	jwtSecretBase64 := utils.GetEnv(utils.EnvPantahubJWTAuthSecret)
	jwtSecretPem, err := base64.StdEncoding.DecodeString(jwtSecretBase64)
	if err != nil {
		return nil, fmt.Errorf("No valid JWT secret (PANTAHUB_JWT_AUTH_SECRET) in base64 format: %s", err.Error())
	}
	jwtSecret, err := jwt.ParseRSAPrivateKeyFromPEM(jwtSecretPem)
	if err != nil {
		return nil, err
	}
	claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	claims["id"] = account.Prn
	claims["nick"] = account.Nick
	claims["prn"] = account.Prn
	claims["roles"] = "user"
	claims["type"] = "USER"
	claims["scopes"] = "prn:pantahub.com:apis:/base/all"

	tokenString, err := token.SignedString(jwtSecret)

	return &TokenPayload{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    "prn:pantahub.com:apis:/base/all",
	}, err
}

func isDubplicateKey(key string, err error) bool {
	return strings.Contains(err.Error(), "duplicate key error collection") &&
		strings.Contains(err.Error(), "index: "+key)
}
