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
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (a *App) getTokenUsingPassword(c *echo.Context) error {
	userAgent := c.Request().Header.Get("User-Agent")
	if userAgent == "" {
		return echoutil.RestErrorWrapperUser(c, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
	}

	payload := &authmodels.LoginRequestPayload{}

	// Prefer Authorization: Basic if present.
	authz := c.Request().Header.Get("Authorization")
	if strings.HasPrefix(authz, "Basic ") {
		user, pass, ok := c.Request().BasicAuth()
		if ok && user != "" {
			payload.Username = user
			payload.Password = pass
		}
	}

	// Fall back to JSON body when no Basic auth credentials were extracted.
	if payload.Username == "" {
		err := echoutil.DecodeJsonPayload(c, payload)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Failed to decode token Request", http.StatusBadRequest)
		}
	}

	// accounts with two-factor authentication enabled get an MFA challenge
	// instead of a session token (personal access tokens stay single-step)
	if handled := a.maybeStartMFALogin(c, payload); handled {
		return nil
	}

	tokenString, rerr := authservices.CreateUserToken(payload, a.jwtConfig, a.mongoClient)
	if rerr != nil {
		return echoutil.RestErrorWrite(c, rerr)
	}

	if tokenString == "" {
		rerr = &utils.RError{
			Msg:   fmt.Sprintf("can get token for %s", payload.Username),
			Error: "Authentication Failed",
			Code:  http.StatusUnauthorized,
		}
		return echoutil.RestErrorWrite(c, rerr)
	}

	return echoutil.WriteJSON(c, http.StatusOK, authmodels.TokenResponse{
		Token: tokenString,
	})

}
