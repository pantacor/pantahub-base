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
	"net/http"
	"regexp"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

// handlePatchDevice update a device
// @Summary update a device
// @Description update a device
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body Device true "Device payload"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id} [patch]
func (a *App) handlePatchDevice(c *echo.Context) error {
	newDevice := Device{}
	patchID := c.Param("id")

	authID, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in with a known authentication type.", http.StatusForbidden)
	}

	if authType != "USER" && authType != "SESSION" && !strings.HasSuffix(authID.(string), "/"+patchID) {
		return echoutil.RestErrorWrapper(c, "Devices can only change their own nick.", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	deviceID, err := primitive.ObjectIDFromHex(patchID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)
	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if newDevice.Owner == "" || (authType == "USER" && newDevice.Owner != authID) ||
		(authType == "SESSION" && newDevice.Owner != authID) {
		return echoutil.RestErrorWrapper(c, "Not User/Device Accessible Resource Id", http.StatusForbidden)
	}

	patch := Device{}
	patched := false

	err = echoutil.DecodeJsonPayload(c, &patch)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Internal Error (decode patch)", http.StatusInternalServerError)
	}
	if patch.Nick != "" {
		newDevice.Nick = patch.Nick
		patched = true
	}
	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
	}
	if !isValidNick {
		return echoutil.RestErrorWrapper(c, "Invalid Device Nick(Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
	}

	if patched {
		newDevice.TimeModified = time.Now()
		_, err = collection.UpdateOne(
			ctx,
			bson.M{"_id": newDevice.ID},
			bson.M{"$set": bson.M{
				"nick":         newDevice.Nick,
				"timemodified": newDevice.TimeModified,
			}},
		)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error updating patched device state", http.StatusForbidden)
		}
	}

	newDevice.Challenge = ""
	newDevice.Secret = ""

	return echoutil.WriteJSON(c, http.StatusOK, newDevice)
}
