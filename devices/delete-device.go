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
	"log"
	"net/http"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// handleDeleteDevice Mark a device to be deleted by device garbage collector
// @Summary Mark a device to be deleted by device garbage collector
// @Description Mark a device to be deleted by device garbage collector
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id} [delete]
func (a *App) handleDeleteDevice(c *echo.Context) error {
	delID := c.Param("id")

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in as a USER", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	device := Device{}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(delID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Println("Error deleting device: " + err.Error())
			return echoutil.RestErrorWrapper(c, "Device not found", http.StatusInternalServerError)
		}

		device.ID = deviceObjectID
		return echoutil.WriteJSON(c, http.StatusOK, device)
	}

	// Any logged-in caller may delete an unclaimed device (no owner yet);
	// pantahub-gc would sweep it anyway. Owned devices need their owner.
	if device.Owner != "" && device.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	result, res, err := MarkDeviceAsGarbage(delID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error calling GC API for Marking Device Garbage: "+err.Error(), http.StatusInternalServerError)
	}

	if res.StatusCode() != 200 {
		log.Printf("GC API error marking device %s garbage: status %d", delID, res.StatusCode())
		return echoutil.RestErrorWrapper(c, "Error calling GC API for Marking Device Garbage", http.StatusInternalServerError)
	}
	if result.Status == 1 {
		device.Garbage = true
	}

	device.Secret = ""
	device.Challenge = ""
	return echoutil.WriteJSON(c, http.StatusOK, device)
}
