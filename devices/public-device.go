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
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// handlePutPublic Make a device public
// @Summary Make a device public
// @Description Make a device public
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/public [put]
func (a *App) handlePutPublic(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	putID := r.PathParam("id")

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
	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner != "" && newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	newDevice.IsPublic = true
	newDevice.TimeModified = time.Now()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": newDevice},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newDevice)
}

// handleDeletePublic Make a device private
// @Summary Make a device private
// @Description Make a device private
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/public [delete]
func (a *App) handleDeletePublic(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	putID := r.PathParam("id")

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

	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner != "" && newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	newDevice.IsPublic = false
	newDevice.TimeModified = time.Now()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": newDevice},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newDevice)
}
