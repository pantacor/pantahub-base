// Copyright 2020  Pantacor Ltd.
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
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type loginRequestPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Scope    string `json:"scope"`
}

func (a *App) getTokenUsingPassword(writer rest.ResponseWriter, r *rest.Request) {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return
	}

	payload := &loginRequestPayload{}
	err := r.DecodeJsonPayload(payload)
	if err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode token Request", http.StatusBadRequest)
		return
	}

	var scopes []string
	if payload.Scope != "" && payload.Username == accountsdata.AnonAccountDefaultUsername {
		scopes = utils.ScopeStringFilterBy(strings.Fields(payload.Scope), ".readonly", "")
	} else {
		scopes = utils.ScopeStringFilterBy(strings.Fields(payload.Scope), "", "")
	}

	if payload.Username != accountsdata.AnonAccountDefaultUsername && !a.jwtMiddleware.Authenticator(payload.Username, payload.Password) {
		utils.RestErrorWrapper(writer, "Authentication Failed", http.StatusBadRequest)
		return
	}

	token := jwt.New(jwt.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm))
	claims := token.Claims.(jwt.MapClaims)

	if a.jwtMiddleware.PayloadFunc != nil {
		for key, value := range a.jwtMiddleware.PayloadFunc(payload.Username) {
			claims[key] = value
		}
	}

	if payload.Username != accountsdata.AnonAccountDefaultUsername {
		claims["id"] = payload.Username
	}

	claims["exp"] = time.Now().Add(a.jwtMiddleware.Timeout).Unix()

	if len(scopes) > 0 {
		claims["scopes"] = strings.Join(scopes, " ")
	}

	if payload.Username == accountsdata.AnonAccountDefaultUsername {
		timeoutStr := utils.GetEnv(utils.EnvAnonJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 5
		}
		claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	}

	if a.jwtMiddleware.MaxRefresh != 0 {
		claims["orig_iat"] = time.Now().Unix()
	}
	tokenString, err := token.SignedString(a.jwtMiddleware.Key)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error signing new token", http.StatusInternalServerError)
		return
	}

	writer.WriteJson(tokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    claims["scopes"].(string),
	})

}
