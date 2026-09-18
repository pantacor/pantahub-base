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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"net/http"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// handleDeleteApp delete an oauth client
// @Summary delete an oauth client
// @Description delete an oauth client
// @Accept  json
// @Produce  json
// @Tags apps
// @Security ApiKeyAuth
// @Param id path string true "App ID|Nick|PRN"
// @Success 200 {object} TPApp
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps/{id} [delete]
func (app *App) handleDeleteApp(c *echo.Context) error {
	id := c.Param("id")

	var owner string
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusInternalServerError)
	}

	database := app.mongoClient.Database(utils.MongoDb)
	tpApp, httpCode, err := SearchApp(c.Request().Context(), owner, id, database)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), httpCode)
	}

	now := time.Now()
	tpApp.DeletedAt = &now
	tpApp.TimeModified = time.Now()
	_, err = CreateOrUpdateApp(c.Request().Context(), tpApp, database)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), httpCode)
	}

	return echoutil.WriteJSON(c, http.StatusOK, tpApp)
}
