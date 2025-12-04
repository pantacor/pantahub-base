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
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/decoder"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

type metaDataPayload map[string]interface{}

// handlePutDeviceData Update device metadata using the device credentials:
// @Summary Update device metadata using the device credentials:
// @Description Update device metadata using the device credentials:
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
// @Router /devices/{id}/device-meta [put]
func (a *App) handlePutDeviceData(w rest.ResponseWriter, r *rest.Request) {

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

	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Device data can only be updated by Device", http.StatusBadRequest)
		return
	}

	deviceID := r.PathParam("id")
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{}
	err = decoder.DecodeJsonPayload(r, &data)
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var device Device
	err = collection.FindOne(ctx, bson.M{
		"_id": deviceObjectID,
		"prn": owner.(string),
	}).Decode(&device)
	if err != nil && mongoutils.IsNotFound(err) {
		utils.RestErrorWrapper(w, "Device not found", http.StatusNotFound)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id": deviceObjectID,
			"prn": owner.(string),
		},
		bson.M{"$set": bson.M{
			"device-meta":  data,
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

	w.WriteJson(map[string]string{"status": "ok"})
}

var parsingErrorKey = "hub_parsing"

// handlePatchDeviceData Update device metadata using the device credentials:
// @Summary Update device metadata using the device credentials:
// @Description Update device metadata using the device credentials:
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID"
// @Param body body metaDataPayload true "Device meta payload"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/device-meta [patch]
func (a *App) handlePatchDeviceData(w rest.ResponseWriter, r *rest.Request) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	device := Device{}

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
		return
	}

	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Device data can only be updated by Device", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	callerStr, ok := caller.(string)
	if !ok {
		utils.RestErrorWrapper(w, "Owner state not set.", http.StatusInternalServerError)
	}

	deviceID := r.PathParam("id")
	if deviceID == "" || !strings.HasSuffix(callerStr, "/"+deviceID) {
		utils.RestErrorWrapper(w, "Calling Device "+callerStr+"and Device ID "+deviceID+" in url mismatch.", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, bson.M{
		"prn":     callerStr,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil && mongoutils.IsNotFound(err) {
		utils.RestErrorWrapper(w, "Device not found", http.StatusNotFound)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	data := map[string]interface{}{}
	content, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		utils.RestErrorWrapper(w, "Error reading request device-meta body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(content) == 0 {
		utils.RestErrorWrapper(w, "Request device-meta body is empty", http.StatusBadRequest)
		return
	}

	err = json.Unmarshal(content, &data)
	if err != nil {
		device.DeviceMeta[parsingErrorKey] = map[string]string{
			"error":     err.Error(),
			"content":   string(content),
			"timestamp": time.Now().Format(time.RFC3339),
		}
	} else {
		for k, v := range data {
			device.DeviceMeta[k] = v
			if v == nil {
				delete(device.DeviceMeta, k)
			}
		}

	}

	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"prn": callerStr,
		},
		bson.M{"$set": bson.M{
			"device-meta":  device.DeviceMeta,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(map[string]string{"status": "ok"})
}
