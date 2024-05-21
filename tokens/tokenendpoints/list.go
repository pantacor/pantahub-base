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
	"fmt"
	"net/http"
	"net/url"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/querymongo"
)

// ListTokens List tokens for a owner
// @Summary List tokens for a owner
// @Description List tokens for a owner
// @Tags tokens
// @Accept  json
// @Produce json
// @Param   owner 	  query   string     true        "Owner"
// @Param   limit     query   int        false       "Limit"
// @Param   offset    query   int        false       "Offset"
// @Param   sort      query   string     false       "Sort"
// @Param   createdAt query   string     false       "CreatedAt"
// @Success 200 {array} tokenservice.ListOfToken
// @Failure 400 {object} utils.RError "Bad Request"
// @Failure 403 {object} utils.RError "Forbidden"
// @Failure 404 {object} utils.RError "Not Found"
// @Failure 500 {object} utils.RError "Internal Server Error"
// @Router /tokens [get]
func (app *Endpoints) ListTokens(w rest.ResponseWriter, r *rest.Request) {
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

	asp := querymongo.GetAllQueryPagination(r.URL, nil)
	aspUrl, err := url.Parse(
		fmt.Sprintf(
			"%s://%s:%s%s",
			utils.GetEnv(utils.EnvPantahubScheme),
			utils.GetEnv(utils.EnvPantahubHost),
			utils.GetEnv(utils.EnvPantahubPort),
			r.RequestURI,
		),
	)
	if err == nil {
		asp.Url = *aspUrl
	}

	response, err := app.service.GetTokens(r.Context(), owner.(string), asp)
	if err != nil {
		utils.RestErrorWrapper(w, "token owner is not owner of the device -- "+err.Error(), http.StatusForbidden)
		return
	}

	if err := w.WriteJson(response); err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
