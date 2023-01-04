//
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

package devices

import (
	"context"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo/options"

	"gopkg.in/mgo.v2/bson"
)

// handleGetTokens Get all device tokens
// @Summary Get all device tokens
// @Description Get all device tokens
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {object} utils.PantahubDevicesJoinToken
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens [get]
func (a *App) handleGetTokens(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
		return
	}

	if authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	res := []utils.PantahubDevicesJoinToken{}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	findOptions := options.Find()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, bson.M{
		"owner": caller.(string),
	}, findOptions)

	if err != nil {
		utils.RestErrorWrapper(w, "error getting device tokens for user:"+err.Error(), http.StatusForbidden)
		return
	}

	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := utils.PantahubDevicesJoinToken{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		// lets not reveal details about token when collection gets queried
		result.TokenSha = nil
		result.Token = ""
		res = append(res, result)
	}

	w.WriteJson(res)
}
