// Copyright 2024  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package tokenendpoints

import (
	"net/http"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// GetToken get a token for a user
// @Summary get a token for a user
// @Description get a token for a user
// @Accept json
// @Produce json
// @Tags tokens
// @Security ApiKeyAuth
// @Param Authorization header string true "Insert your access token" default(Bearer <Add access token here>)
// @Param id path string true "Token ID"
// @Success 200 {object} tokenmodels.AuthToken
// @Failure 400 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /tokens/{id} [get]
func (app *Endpoints) GetToken(w rest.ResponseWriter, r *rest.Request) {
	var owner interface{}

	if jwtPayload, ok := r.Env["JWT_PAYLOAD"]; ok {
		if owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]; !ok {
			utils.RestErrorWrapper(w, "Owner can't be defined", http.StatusBadRequest)
			return
		}
	} else {
		utils.RestErrorWrapper(w, "Owner can't be defined", http.StatusBadRequest)
		return
	}

	id := r.PathParam("id")
	response, err := app.service.GetToken(r.Context(), id, owner.(string))
	if err != nil {
		utils.RestErrorWrapper(w, "token owner is not owner of the device -- "+err.Error(), http.StatusForbidden)
		return
	}

	response.Secret = ""

	if err := w.WriteJson(response); err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
