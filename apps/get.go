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
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// handleGetApp get an oauth client
// @Summary Get an oauth application
// @Description Get an oauth application
// @Accept  json
// @Produce  json
// @Tags apps
// @Security ApiKeyAuth
// @Param id path string true "App ID|Nick|PRN"
// @Success 200 {object} TPApp
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps/{id} [get]
func (app *App) handleGetApp(c *echo.Context) error {
	id := c.Param("id")

	var owner string
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
		if !ok {
			return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusInternalServerError)
		}
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusInternalServerError)
	}

	tpApp, httpCode, err := SearchApp(c.Request().Context(), "", id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), httpCode)
	}

	if tpApp == nil {
		return echoutil.RestErrorWrapper(c, "App not found", http.StatusNotFound)
	}

	if tpApp.Owner != owner {
		now := time.Now()
		tpApp.Type = ""
		tpApp.Secret = ""
		tpApp.ID = primitive.NilObjectID
		tpApp.DeletedAt = &now
		tpApp.TimeModified = time.Now()
		tpApp.TimeCreated = time.Now()
		tpApp.ExposedScopes = []utils.Scope{}
		tpApp.ExposedScopesLength = 0
		tpApp.RedirectURIs = []string{}
		tpApp.Owner = ""
		tpApp.OwnerNick = ""
	}

	return echoutil.WriteJSON(c, http.StatusOK, tpApp)
}

// handleGetApps get an oauth clients
// @Summary Get all applications owned by a user
// @Description Get all applications owned by a user
// @Accept  json
// @Produce  json
// @Tags apps
// @Security ApiKeyAuth
// @Param serviceID query string true "App ID|Nick|PRN"
// @Success 200 {array} TPApp
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps [get]
func (app *App) handleGetApps(c *echo.Context) error {
	id := c.Request().URL.Query().Get("serviceID")

	owner := ""
	var sessionOwner string
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if ok {
		sessionOwner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
		if !ok {
			return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusInternalServerError)
		}
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusInternalServerError)
	}

	if id != "" {
		owner = ""
	} else {
		owner = sessionOwner
	}
	apps, err := SearchApps(c.Request().Context(), owner, id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error reading third party application "+err.Error(), http.StatusInternalServerError)
	}

	for i, app := range apps {
		if app.Owner != sessionOwner {
			now := time.Now()
			apps[i].Type = ""
			apps[i].Secret = ""
			apps[i].ID = primitive.NilObjectID
			apps[i].DeletedAt = &now
			apps[i].TimeModified = time.Now()
			apps[i].TimeCreated = time.Now()
			apps[i].ExposedScopes = []utils.Scope{}
			apps[i].ExposedScopesLength = 0
			apps[i].RedirectURIs = []string{}
			apps[i].Owner = ""
			apps[i].OwnerNick = ""
		}
	}

	return echoutil.WriteJSON(c, http.StatusOK, apps)
}

// @Summary Get scopes for OAuth applications
// @Description Get scopes for OAuth applications
// @Accept  json
// @Produce  json
// @Tags apps
// @Param serviceID query string false "ID|Nick|PRN"
// @Success 200 {array} utils.Scope
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps/scopes [get]
func (app *App) handleGetPhScopes(c *echo.Context) error {
	id := c.Request().URL.Query().Get("serviceID")

	if id == "" {
		scopes, err := SearchExposedScopes(c.Request().Context(), app.mongoClient.Database(utils.MongoDb))
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
		}
		return echoutil.WriteJSON(c, http.StatusOK, append(utils.PhScopeArray, scopes...))
	}

	tpApp, httpCode, err := SearchApp(c.Request().Context(), "", id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), httpCode)
	}

	if tpApp == nil {
		return echoutil.RestErrorWrapper(c, "App not found", http.StatusNotFound)
	}

	return echoutil.WriteJSON(c, http.StatusOK, tpApp.Scopes)
}
