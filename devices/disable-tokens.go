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
	"fmt"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// handleDisableTokens Disable a device token in order to be unable to used as authetication
// @Summary Disable a device token in order to be unable to used as authetication
// @Description Disable a device token in order to be unable to used as authetication
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|Nick|PRN"
// @Success 200 {object} disableToken
// @Failure 400 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens/{id} [delete]
func (a *App) handleDisableTokens(w rest.ResponseWriter, r *rest.Request) {

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
		utils.RestErrorWrapper(w, "Can not be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	r.ParseForm()
	tokenID := r.PathParam("id")
	tokenIDBson, err := primitive.ObjectIDFromHex(tokenID)
	if err != nil {
		message := fmt.Sprintf("error decoding id to ObjectID: %s -- %s", tokenID, err.Error())
		utils.RestErrorWrapper(w, message, http.StatusInternalServerError)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   tokenIDBson,
			"owner": caller.(string),
		},
		bson.M{"$set": bson.M{"disabled": true}},
		updateOptions,
	)

	if err != nil {
		utils.RestErrorWrapper(w, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(bson.M{"status": "OK"})
}
