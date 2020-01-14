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
func (app *App) handleGetApp(w rest.ResponseWriter, r *rest.Request) {
	id := r.PathParam("id")

	var owner string
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		rest.Error(w, "Owner can't be defined", http.StatusInternalServerError)
		return
	}

	tpApp, httpCode, err := SearchApp(owner, id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		rest.Error(w, err.Error(), httpCode)
		return
	}

	if tpApp == nil {
		rest.Error(w, "App not found", http.StatusNotFound)
		return
	}

	w.WriteJson(tpApp)
}

// handleGetApps get an oauth clients
func (app *App) handleGetApps(w rest.ResponseWriter, r *rest.Request) {
	var owner string
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		rest.Error(w, "Owner can't be defined", http.StatusInternalServerError)
		return
	}

	apps, err := SearchApps(owner, "", app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		rest.Error(w, "Error reading third party application "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(apps)
}

func (app *App) handleGetPhScopes(w rest.ResponseWriter, r *rest.Request) {
	id := r.Request.URL.Query().Get("serviceID")

	if id == "" {
		scopes, err := SearchExposedScopes(app.mongoClient.Database(utils.MongoDb))
		if err != nil {
			rest.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteJson(append(utils.PhScopeArray, scopes...))
		return
	}

	tpApp, httpCode, err := SearchApp("", id, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		rest.Error(w, err.Error(), httpCode)
		return
	}

	if tpApp == nil {
		rest.Error(w, "App not found", http.StatusNotFound)
		return
	}

	w.WriteJson(tpApp.Scopes)
}