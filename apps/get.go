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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"net/http"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// handleGetApp get an oauth client
// @Summary Get an oauth application
// @Description Get an oauth application
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Param id path string true "App ID|Nick|PRN"
// @Success 200 {object} TPApp
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps/{id} [get]
func (app *App) handleGetApp(w rest.ResponseWriter, r *rest.Request) {
	id := r.PathParam("id")

	var owner string
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		utils.RestErrorWrapper(w, "Owner can't be defined", http.StatusInternalServerError)
		return
	}

	tpApp, httpCode, err := SearchApp(owner, id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), httpCode)
		return
	}

	if tpApp == nil {
		utils.RestErrorWrapper(w, "App not found", http.StatusNotFound)
		return
	}

	w.WriteJson(tpApp)
}

// handleGetApps get an oauth clients
// @Summary Get all applications owned by a user
// @Description Get all applications owned by a user
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Param id path string true "App ID|Nick|PRN"
// @Success 200 {array} TPApp
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps [get]
func (app *App) handleGetApps(w rest.ResponseWriter, r *rest.Request) {
	var owner string
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		utils.RestErrorWrapper(w, "Owner can't be defined", http.StatusInternalServerError)
		return
	}

	apps, err := SearchApps(owner, "", app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		utils.RestErrorWrapper(w, "Error reading third party application "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(apps)
}

// @Summary Get scopes for OAuth applications
// @Description Get scopes for OAuth applications
// @Accept  json
// @Produce  json
// @Param serviceID query string false "ID|Nick|PRN"
// @Success 200 {array} utils.Scope
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "App not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /apps/scopes [get]
func (app *App) handleGetPhScopes(w rest.ResponseWriter, r *rest.Request) {
	id := r.Request.URL.Query().Get("serviceID")

	if id == "" {
		scopes, err := SearchExposedScopes(app.mongoClient.Database(utils.MongoDb))
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteJson(append(utils.PhScopeArray, scopes...))
		return
	}

	tpApp, httpCode, err := SearchApp("", id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), httpCode)
		return
	}

	if tpApp == nil {
		utils.RestErrorWrapper(w, "App not found", http.StatusNotFound)
		return
	}

	w.WriteJson(tpApp.Scopes)
}
