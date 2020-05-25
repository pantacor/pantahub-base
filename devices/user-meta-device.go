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
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

// handlePatchUserData Update user metadata using the user credentials:
// @Summary Update user metadata using the user credentials:
// @Description Update user metadata using the user credentials:
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body metaDataPayload true "Device payload"
// @Success 200 {object} metaDataPayload
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/user-meta [patch]
func (a *App) handlePatchUserData(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "USER" {
		utils.RestErrorWrapper(w, "User data can only be updated by User", http.StatusBadRequest)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		utils.RestErrorWrapper(w, "Session has no valid caller/owner info.", http.StatusBadRequest)
		return
	}

	deviceID, err := a.ResolveDeviceIDOrNick(ownerStr, r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}
	data := map[string]interface{}{}
	err = r.DecodeJsonPayload(&data)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var device Device
	err = collection.FindOne(ctx,
		bson.M{
			"_id":     deviceID,
			"garbage": bson.M{"$ne": true},
		}).
		Decode(&device)

	for k, v := range data {
		device.UserMeta[k] = v
	}

	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   deviceID,
			"owner": owner.(string),
		},
		bson.M{"$set": bson.M{
			"user-meta":    device.UserMeta,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device user-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}

// handlePutUserData Update user metadata using the user credentials
// @Summary Update user metadata using the user credentials
// @Description Update user metadata using the user credentials
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body metaDataPayload true "Device payload"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/user-meta [put]
func (a *App) handlePutUserData(w rest.ResponseWriter, r *rest.Request) {
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "USER" {
		utils.RestErrorWrapper(w, "User data can only be updated by User", http.StatusBadRequest)
		return
	}

	deviceID := r.PathParam("id")

	data := map[string]interface{}{}
	err := r.DecodeJsonPayload(&data)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   deviceObjectID,
			"owner": owner.(string),
		},
		bson.M{"$set": bson.M{
			"user-meta":    data,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device user-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}
