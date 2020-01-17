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
	"regexp"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// handlePatchDevice update a device
// @Summary update a device
// @Description update a device
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Param id path string true "ID|PRN|NICK"
// @Param body body Device true "Device payload"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id} [patch]
func (a *App) handlePatchDevice(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	patchID := r.PathParam("id")

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" {
		utils.RestErrorWrapper(w, "Devices cannot change their own public state.", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceID, err := primitive.ObjectIDFromHex(patchID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner == "" || newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	patch := Device{}
	patched := false

	err = r.DecodeJsonPayload(&patch)

	if err != nil {
		utils.RestErrorWrapper(w, "Internal Error (decode patch)", http.StatusInternalServerError)
		return
	}
	if patch.Nick != "" {
		newDevice.Nick = patch.Nick
		patched = true
	}
	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isValidNick {
		utils.RestErrorWrapper(w, "Invalid Device Nick(Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
		return
	}

	if patched {
		newDevice.TimeModified = time.Now()
		updateOptions := options.Update()
		updateOptions.SetUpsert(true)
		_, err = collection.UpdateOne(
			ctx,
			bson.M{"_id": newDevice.ID},
			bson.M{"$set": newDevice},
			updateOptions,
		)
		if err != nil {
			utils.RestErrorWrapper(w, "Error updating patched device state", http.StatusForbidden)
			return
		}
	}

	newDevice.Challenge = ""
	newDevice.Secret = ""

	w.WriteJson(newDevice)
}
