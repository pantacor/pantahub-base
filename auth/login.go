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
	"fmt"
	"net/http"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// @Summary Get login token using username and password
// @Description Get login token using username and password. Supports both JSON body credentials and HTTP Basic Auth header. When Authorization: Basic is present, it takes precedence over the JSON body.
// @Accept  json
// @Produce  json
// @Tags auth
// @Security BasicAuth
// @Param body body authmodels.LoginRequestPayload false "Login credentials (required if not using Basic Auth)"
// @Success 200 {object} authmodels.TokenResponse
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/login [post]
func (a *App) getTokenUsingPassword(writer rest.ResponseWriter, r *rest.Request) {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return
	}

	payload := &authmodels.LoginRequestPayload{}

	// Prefer Authorization: Basic if present.
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(authz, "Basic ") {
		user, pass, ok := r.Request.BasicAuth()
		if ok && user != "" {
			payload.Username = user
			payload.Password = pass
		}
	}

	// Fall back to JSON body when no Basic auth credentials were extracted.
	if payload.Username == "" {
		err := r.DecodeJsonPayload(payload)
		if err != nil {
			utils.RestErrorWrapper(writer, "Failed to decode token Request", http.StatusBadRequest)
			return
		}
	}

	// accounts with two-factor authentication enabled get an MFA challenge
	// instead of a session token (personal access tokens stay single-step)
	if handled := a.maybeStartMFALogin(writer, r, payload); handled {
		return
	}

	tokenString, rerr := authservices.CreateUserToken(payload, a.jwtMiddleware, a.mongoClient)
	if rerr != nil {
		utils.RestErrorWrite(writer, rerr)
		return
	}

	if tokenString == "" {
		rerr = &utils.RError{
			Msg:   fmt.Sprintf("can get token for %s", payload.Username),
			Error: "Authentication Failed",
			Code:  http.StatusUnauthorized,
		}
		utils.RestErrorWrite(writer, rerr)
		return
	}

	writer.WriteJson(authmodels.TokenResponse{
		Token: tokenString,
	})

}
