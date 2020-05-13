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
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	petname "github.com/dustinkirkland/golang-petname"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"gopkg.in/mgo.v2/bson"
)

// handlePostDevice Create a new device for an account
// @Summary Create a new device for an account
// @Description Create a new device for an account
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param body body Device true "Device payload"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices [post]
func (a *App) handlePostDevice(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	r.DecodeJsonPayload(&newDevice)

	mgoid := bson.NewObjectId()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newDevice.ID = ObjectID
	newDevice.Prn = "prn:::devices:/" + newDevice.ID.Hex()

	// if user does not provide a secret, we invent one ...
	if newDevice.Secret == "" {
		var err error
		newDevice.Secret, err = utils.GenerateSecret(15)
		if err != nil {
			utils.RestErrorWrapper(w, "Error generating secret", http.StatusInternalServerError)
			return
		}
	}

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]

	var owner interface{}
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	}

	if ok {
		// user registering here...
		newDevice.Owner = owner.(string)
		newDevice.UserMeta = utils.BsonQuoteMap(&newDevice.UserMeta)
		newDevice.DeviceMeta = map[string]interface{}{}
	} else {

		// device speaking here...
		newDevice.Owner = ""
		newDevice.UserMeta = map[string]interface{}{}
		newDevice.DeviceMeta = utils.BsonQuoteMap(&newDevice.DeviceMeta)

		// lets see if we have an auto assign candidate
		autoAuthToken := r.Header.Get(PantahubDevicesAutoTokenV1)

		if autoAuthToken != "" {

			autoInfo, err := a.getBase64AutoTokenInfo(autoAuthToken)
			if err != nil {
				utils.RestErrorWrapper(w, "Error using AutoAuthToken "+err.Error(), http.StatusBadRequest)
				return
			}

			// update owner and usermeta
			newDevice.Owner = autoInfo.Owner
			if autoInfo.UserMeta != nil {
				newDevice.UserMeta = autoInfo.UserMeta
			}

		} else {
			newDevice.Challenge = petname.Generate(3, "-")
		}
	}

	newDevice.TimeCreated = time.Now()
	newDevice.TimeModified = newDevice.TimeCreated

	// wecreate a random name for unregistered devices;
	// registry controllers are expected to change these when
	// device gets associated with owner
	// if we have an owner, we assign proper nick
	if newDevice.Owner != "" && newDevice.Nick == "" {
		newDevice.Nick = petname.Generate(3, "_")
	} else if newDevice.Nick == "" {
		newDevice.Nick = "__unregistered__" + petname.Generate(1, "_") + "_" + utils.RandStringLower(10)
	}

	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isValidNick {
		utils.RestErrorWrapper(w, "Invalid Device Nick (Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": ObjectID},
		bson.M{"$set": newDevice},
		updateOptions,
	)

	if err != nil {
		log.Print(newDevice)
		utils.RestErrorWrapper(w, "Error creating device "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(newDevice)
}
