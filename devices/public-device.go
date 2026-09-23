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
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

// handlePutPublic Make a device public
// @Summary Make a device public
// @Description Make a device public
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/public [put]
func (a *App) handlePutPublic(c *echo.Context) error {
	newDevice := Device{}
	putID := c.Param("id")

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

	if authType == "DEVICE" {
		return echoutil.RestErrorWrapper(c, "Devices cannot change their own public state.", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)

	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if newDevice.Owner != "" && newDevice.Owner != authID {
		return echoutil.RestErrorWrapper(c, "Not User Accessible Resource Id", http.StatusForbidden)
	}

	wasPublic := newDevice.IsPublic
	newDevice.IsPublic = true
	newDevice.TimeModified = time.Now()

	setFields := bson.M{
		"ispublic":     newDevice.IsPublic,
		"timemodified": newDevice.TimeModified,
	}
	if !wasPublic {
		// clear the flag so the kafka listener re-syncs steps
		setFields["mark_public_processed"] = false
	}

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": setFields},
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating device public state", http.StatusForbidden)
	}

	return echoutil.WriteJSON(c, http.StatusOK, newDevice)
}

// handleDeletePublic Make a device private
// @Summary Make a device private
// @Description Make a device private
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/public [delete]
func (a *App) handleDeletePublic(c *echo.Context) error {
	newDevice := Device{}
	putID := c.Param("id")

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

	if authType == "DEVICE" {
		return echoutil.RestErrorWrapper(c, "Devices cannot change their own public state.", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}

	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)
	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if newDevice.Owner != "" && newDevice.Owner != authID {
		return echoutil.RestErrorWrapper(c, "Not User Accessible Resource Id", http.StatusForbidden)
	}

	wasPublic := newDevice.IsPublic
	newDevice.IsPublic = false
	newDevice.TimeModified = time.Now()

	setFields := bson.M{
		"ispublic":     newDevice.IsPublic,
		"timemodified": newDevice.TimeModified,
	}
	if wasPublic {
		// clear the flag so the kafka listener re-syncs steps
		setFields["mark_public_processed"] = false
	}

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": setFields},
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating device public state", http.StatusForbidden)
	}

	return echoutil.WriteJSON(c, http.StatusOK, newDevice)
}
