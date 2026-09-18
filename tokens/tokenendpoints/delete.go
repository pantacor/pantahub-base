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
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// DeleteToken Delete a token for a user
// @Summary Delete a token for a user
// @Description Delete a token for a user
// @Accept json
// @Produce json
// @Tags tokens
// @Security ApiKeyAuth
// @Param Authorization header string true "Insert your access token" default(Bearer <Add access token here>)
// @Param id path string true "Token ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /tokens/{id} [delete]
func (app *Endpoints) DeleteToken(c *echo.Context) error {
	var owner interface{}

	if jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload); ok {
		if owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]; !ok {
			return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
		}
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
	}

	id := c.Param("id")
	err := app.service.DeleteToken(c.Request().Context(), id, owner.(string))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "token owner is not owner of the device -- "+err.Error(), http.StatusForbidden)
	}

	response := map[string]interface{}{"success": true}
	if err := echoutil.WriteJSON(c, http.StatusOK, response); err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
	}
	return nil
}
