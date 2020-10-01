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
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (app *App) handleDeleteApp(w rest.ResponseWriter, r *rest.Request) {
	id := r.PathParam("id")

	var owner string
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	} else {
		utils.RestErrorWrapper(w, "Owner can't be defined", http.StatusInternalServerError)
		return
	}

	database := app.mongoClient.Database(utils.MongoDb)
	tpApp, httpCode, err := SearchApp(owner, id, database)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), httpCode)
		return
	}

	now := time.Now()
	tpApp.DeletedAt = &now
	tpApp.TimeModified = time.Now()
	_, err = CreateOrUpdateApp(tpApp, database)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), httpCode)
		return
	}

	w.WriteJson(tpApp)
}
