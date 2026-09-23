//
// Copyright (c) 2017-2026 Pantacor Ltd.
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

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/decoder"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (a *App) handlePutDeviceData(c *echo.Context) error {

	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD", http.StatusBadRequest)
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
	}

	if authType != "DEVICE" {
		return echoutil.RestErrorWrapper(c, "Device data can only be updated by Device", http.StatusBadRequest)
	}

	deviceID := c.Param("id")
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}

	data := map[string]interface{}{}
	err = decoder.DecodeJsonBody(c.Request().Body, &data)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error parsing data: "+err.Error(), http.StatusBadRequest)
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// For PUT, we replace the whole metadata object.
	// To do this atomically without clobbering other fields in the device document,
	// we still use $set but on the whole "device-meta" field.
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id": deviceObjectID,
			"prn": owner.(string),
		},
		bson.M{"$set": bson.M{
			"device-meta":   data,
			"timemodified":  time.Now(),
			"meta-modified": time.Now(),
		}},
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating device metadata: "+err.Error(), http.StatusBadRequest)
	}
	if updateResult.MatchedCount == 0 {
		return echoutil.RestErrorWrapper(c, "Error updating device metadata: not found", http.StatusBadRequest)
	}

	return echoutil.WriteJSON(c, http.StatusOK, map[string]string{"status": "ok"})
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
func (a *App) handlePatchDeviceData(c *echo.Context) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD", http.StatusBadRequest)
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
	}

	if authType != "DEVICE" {
		return echoutil.RestErrorWrapper(c, "Device data can only be updated by Device", http.StatusBadRequest)
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	callerStr, ok := caller.(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Owner state not set.", http.StatusInternalServerError)
	}

	deviceID := c.Param("id")
	if deviceID == "" || !strings.HasSuffix(callerStr, "/"+deviceID) {
		return echoutil.RestErrorWrapper(c, "Calling Device "+callerStr+"and Device ID "+deviceID+" in url mismatch.", http.StatusBadRequest)
	}

	data := map[string]interface{}{}
	content, err := io.ReadAll(c.Request().Body)
	_ = c.Request().Body.Close()
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error reading request device-meta body: "+err.Error(), http.StatusBadRequest)
	}
	if len(content) == 0 {
		return echoutil.RestErrorWrapper(c, "Request device-meta body is empty", http.StatusBadRequest)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	err = json.Unmarshal(content, &data)
	if err != nil {
		// handle parsing error as before
		updateResult, err := collection.UpdateOne(
			ctx,
			bson.M{
				"prn": callerStr,
			},
			bson.M{"$set": bson.M{
				"device-meta." + parsingErrorKey: map[string]string{
					"error":     err.Error(),
					"content":   string(content),
					"timestamp": time.Now().Format(time.RFC3339),
				},
				"timemodified": time.Now(),
			}},
		)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error updating device-meta (parsing error log): "+err.Error(), http.StatusBadRequest)
		}
		if updateResult.MatchedCount == 0 {
			return echoutil.RestErrorWrapper(c, "Error updating device-meta (parsing error log): not found", http.StatusBadRequest)
		}
	} else {
		// 1. Quote the BSON keys first to handle dots in key names (e.g. "lo.ipv4")
		data = utils.BsonQuoteMap(&data)

		// 2. Deep flatten the quoted data to allow atomic nested updates
		setFields := bson.M{}
		unsetFields := bson.M{}
		flattenMap("device-meta", data, setFields, unsetFields)

		// Always update timemodified
		setFields["timemodified"] = time.Now()
		setFields["meta-modified"] = time.Now()

		updateDoc := bson.M{}
		if len(setFields) > 0 {
			updateDoc["$set"] = setFields
		}
		if len(unsetFields) > 0 {
			updateDoc["$unset"] = unsetFields
		}

		updateResult, err := collection.UpdateOne(
			ctx,
			bson.M{
				"prn":     callerStr,
				"garbage": bson.M{"$ne": true},
			},
			updateDoc,
		)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error updating device-meta: "+err.Error(), http.StatusBadRequest)
		}
		if updateResult.MatchedCount == 0 {
			return echoutil.RestErrorWrapper(c, "Error updating device-meta: not found", http.StatusBadRequest)
		}
	}

	return echoutil.WriteJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}
