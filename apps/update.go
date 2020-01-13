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

// handleUpdateApp update a oauth client
func (app *App) handleUpdateApp(w rest.ResponseWriter, r *rest.Request) {
	id := r.PathParam("id")

	payload := &createAppPayload{}
	r.DecodeJsonPayload(payload)

	err := validatePayload(payload)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var owner string
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		rest.Error(w, "Owner can't be defined", http.StatusInternalServerError)
		return
	}

	database := app.mongoClient.Database(utils.MongoDb)
	tpApp, httpCode, err := SearchApp(owner, id, database)
	if err != nil {
		rest.Error(w, err.Error(), httpCode)
		return
	}

	if tpApp == nil {
		rest.Error(w, "App not found", http.StatusNotFound)
		return
	}

	apptype, err := parseType(payload.Type)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.Nick != "" {
		tpApp.Nick = payload.Nick
		tpApp.Prn = utils.BuildScopePrn(payload.Nick)
	}

	scopes, err := parseScopes(payload.Scopes, tpApp.Nick)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if apptype == AppTypePublic {
		tpApp.Secret = ""
	}

	if apptype == AppTypeConfidential && tpApp.Secret == "" {
		tpApp.Secret, err = utils.GenerateSecret(30)
		if err != nil {
			rest.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if apptype == AppTypeConfidential && len(payload.ExposedScopes) > 0 {
		tpApp.ExposedScopes, err = parseScopes(payload.ExposedScopes, payload.Nick)
		if err != nil {
			rest.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	tpApp.RedirectURIs = payload.RedirectURIs
	tpApp.Scopes = scopes
	tpApp.Type = apptype

	_, err = CreateOrUpdateApp(tpApp, database)
	if err != nil {
		rest.Error(w, "Error creating third party application "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(tpApp)
}
