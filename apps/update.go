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

// handleUpdateApp update a oauth client
// @Summary Update a third party application
// @Description Update a third party application
// @Accept  json
// @Produce  json
// @Tags apps
// @Security ApiKeyAuth
// @Param id path string true "App ID|Nick|PRN"
// @Param body body CreateAppPayload true "Update app body"
// @Success 200 {object} TPApp
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps/{id} [put]
func (app *App) handleUpdateApp(c *echo.Context) error {
	id := c.Param("id")

	payload := &CreateAppPayload{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "invalid request body", http.StatusBadRequest)
	}

	err := validatePayload(payload)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
	}

	var owner string
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if ok {
		owner, _ = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusInternalServerError)
	}

	database := app.mongoClient.Database(utils.MongoDb)
	tpApp, httpCode, err := SearchApp(c.Request().Context(), owner, id, database)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), httpCode)
	}

	if tpApp == nil {
		return echoutil.RestErrorWrapper(c, "App not found", http.StatusNotFound)
	}

	apptype, err := parseType(payload.Type)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
	}

	// An application that already has a now built-in nick may keep it.
	if payload.Nick != "" && payload.Nick != tpApp.Nick && reservedNick(payload.Nick) {
		return echoutil.RestErrorWrapperUser(c, "nick is reserved for a built-in client", "nick is reserved for a built-in client", http.StatusConflict)
	}

	if payload.Nick != "" {
		tpApp.Nick = payload.Nick
		tpApp.Prn = utils.BuildScopePrn(payload.Nick)
	}

	scopes, err := parseScopes(payload.Scopes, tpApp.Nick)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
	}

	if apptype == AppTypePublic {
		tpApp.Secret = ""
		tpApp.SecretHash = ""
	}

	// a confidential app without a stored secret hash gets a fresh secret,
	// returned in this response only
	if apptype == AppTypeConfidential && tpApp.SecretHash == "" {
		tpApp.Secret, err = utils.GenerateSecret(30)
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
		}
		tpApp.SecretHash = utils.HashSecret(tpApp.Secret)
	}

	if apptype == AppTypeConfidential && len(payload.ExposedScopes) > 0 {
		tpApp.ExposedScopes, err = parseScopes(payload.ExposedScopes, payload.Nick)
		if err != nil {
			return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
		}
	}

	tpApp.RedirectURIs = payload.RedirectURIs
	tpApp.Scopes = scopes
	tpApp.Type = apptype
	tpApp.Name = payload.Name
	tpApp.Logo = payload.Logo
	tpApp.TimeModified = time.Now()

	_, err = CreateOrUpdateApp(c.Request().Context(), tpApp, database)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error creating third party application "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, tpApp)
}
