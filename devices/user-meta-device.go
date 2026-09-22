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
	"errors"
	"net/http"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/decoder"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
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
func (a *App) handlePatchUserData(c *echo.Context) error {

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

	ownerStr, ok := owner.(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Session has no valid caller/owner info.", http.StatusBadRequest)
	}

	deviceID, err := a.ResolveDeviceIDOrNick(c.Request().Context(), ownerStr, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
	}

	// allow write by USER and SESSION owner, and for the device itself
	if (authType != "USER" && authType != "SESSION") && !strings.HasSuffix(owner.(string), "/"+deviceID.Hex()) {
		return echoutil.RestErrorWrapper(c, "User Meta data can only be patched by owning user/session or the device itself", http.StatusBadRequest)
	}

	data := map[string]interface{}{}
	err = echoutil.DecodeJsonPayload(c, &data)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error parsing data: "+err.Error(), http.StatusBadRequest)
	}

	applied, err := PatchUserMeta(c.Request().Context(), a.mongoClient, owner.(string), *deviceID, data)
	if err != nil {
		if errors.Is(err, ErrDeviceNotOwned) {
			return echoutil.RestErrorWrapper(c, "Error updating device user-meta: not found", http.StatusBadRequest)
		}
		return echoutil.RestErrorWrapper(c, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
	}

	return echoutil.WriteJSON(c, http.StatusOK, applied)
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
func (a *App) handlePutUserData(c *echo.Context) error {
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

	deviceID := c.Param("id")
	if (authType != "USER" && authType != "SESSION") && !strings.HasSuffix(owner.(string), "/"+deviceID) {
		return echoutil.RestErrorWrapper(c, "User data can only be updated by User or the device itself", http.StatusBadRequest)
	}

	data := map[string]interface{}{}
	err := decoder.DecodeJsonBody(c.Request().Body, &data)
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
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}

	query := bson.M{
		"_id": deviceObjectID,
	}

	if authType != "DEVICE" {
		query["owner"] = owner.(string)
	}

	// For PUT, we replace the whole user-meta object.
	// We do this via $set on the specific field to avoid Read-Modify-Write races
	// on the rest of the device document.
	updateResult, err := collection.UpdateOne(
		ctx,
		query,
		bson.M{"$set": bson.M{
			"user-meta":    data,
			"timemodified": time.Now(),
		}},
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
	}
	if updateResult.MatchedCount == 0 {
		return echoutil.RestErrorWrapper(c, "Error updating device user-meta: not found", http.StatusBadRequest)
	}

	return echoutil.WriteJSON(c, http.StatusOK, utils.BsonUnquoteMap(&data))
}

type UserMeta map[string]string

// handleGetUserData get device user metadata
// @Summary get device user metadata
// @Description get device user metadata
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {object} UserMeta
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/user-meta [get]
func (a *App) handleGetUserData(c *echo.Context) error {
	var device Device

	authID, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	ownerPtr := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["owner"]
	if ownerPtr == nil {
		ownerPtr = authID
	}

	owner, ok := ownerPtr.(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Session has no owner info", http.StatusBadRequest)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in with a known authentication type.", http.StatusForbidden)
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	collectionAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collectionAccounts == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database (accounts) connectivity", http.StatusInternalServerError)
	}

	value, useOtherOwnerPrn := c.Request().URL.Query()["owner"]
	if useOtherOwnerPrn {
		ok, err := utils.ValidateUserPrn(value[0])
		if err != nil || !ok {
			return echoutil.RestErrorWrapper(c, "Invalid owner prn", http.StatusForbidden)
		}
		owner = value[0]
	}

	value, useOtherOwnerNick := c.Request().URL.Query()["owner-nick"]
	if useOtherOwnerNick {
		account, err := a.GetUserAccountByNick(c.Request().Context(), value[0])
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
		}
		owner = account.Prn
	}

	mgoid, err := a.ResolveDeviceIDOrNick(c.Request().Context(), owner, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
	}

	query := bson.M{
		"_id":     mgoid,
		"garbage": bson.M{"$ne": true},
	}

	if callerIsUser {
		query["owner"] = authID.(string)
	}
	if callerIsDevice {
		query["prn"] = authID.(string)
	}

	ops := options.FindOne()
	ops.SetProjection(bson.M{
		"prn":       1,
		"owner":     1,
		"garbage":   1,
		"user-meta": 1,
	})

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, query).Decode(&device)
	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	if authID != device.Prn && authID != device.Owner {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	if device.Owner != "" {
		var ownerAccount accounts.Account

		// first check default accounts like user1, user2, etc...
		ownerAccount, ok := accountsdata.DefaultAccounts[device.Owner]
		if !ok {
			ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
			defer cancel()
			err := collectionAccounts.FindOne(ctx,
				bson.M{"prn": device.Owner}).
				Decode(&ownerAccount)

			if err != nil {
				return echoutil.RestErrorWrapper(c, "Owner account not Found", http.StatusInternalServerError)
			}
		}

		profileMeta, _ := a.getProfileMetaData(c.Request().Context(), device.Owner)
		device.UserMeta = utils.MergeMaps(profileMeta, device.UserMeta)
		device.OwnerNick = ownerAccount.Nick
	}

	return echoutil.WriteJSON(c, http.StatusOK, utils.BsonUnquoteMap(&device.UserMeta))
}
