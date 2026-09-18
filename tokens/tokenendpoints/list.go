// Copyright (c) 2017-2026 Pantacor Ltd.
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

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (app *Endpoints) ListTokens(c *echo.Context) error {
	var owner interface{}

	if jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload); ok {
		if owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]; !ok {
			return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
		}
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
	}

	asp := querymongo.GetAllQueryPagination(c.Request().URL, nil)
	aspUrl, err := url.Parse(
		fmt.Sprintf(
			"%s://%s:%s%s",
			utils.GetEnv(utils.EnvPantahubScheme),
			utils.GetEnv(utils.EnvPantahubHost),
			utils.GetEnv(utils.EnvPantahubPort),
			c.Request().RequestURI,
		),
	)
	if err == nil {
		asp.Url = *aspUrl
	}

	response, err := app.service.GetTokens(c.Request().Context(), owner.(string), asp)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "token owner is not owner of the device -- "+err.Error(), http.StatusForbidden)
	}

	if err := echoutil.WriteJSON(c, http.StatusOK, response); err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
	}
	return nil
}
