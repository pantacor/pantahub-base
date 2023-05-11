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

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func (a *App) getTokenUsingPassword(writer rest.ResponseWriter, r *rest.Request) {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
		return
	}

	payload := &authmodels.LoginRequestPayload{}
	err := r.DecodeJsonPayload(payload)
	if err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode token Request", http.StatusBadRequest)
		return
	}

	tokenString, rerr := authservices.CreateUserToken(payload, a.jwtMiddleware)
	if rerr != nil {
		utils.RestErrorWrite(writer, rerr)
		return
	}

	writer.WriteJson(authmodels.TokenResponse{
		Token: tokenString,
	})

}
