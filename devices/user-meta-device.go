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
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/utils"
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

	ownerStr, ok := owner.(string)
	if !ok {
		utils.RestErrorWrapper(w, "Session has no valid caller/owner info.", http.StatusBadRequest)
		return
	}

	deviceID, err := a.ResolveDeviceIDOrNick(r.Context(), ownerStr, r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}

	// allow write by USER and SESSION owner, and for the device itself
	if (authType != "USER" && authType != "SESSION") && !strings.HasSuffix(owner.(string), "/"+deviceID.Hex()) {
		utils.RestErrorWrapper(w, "User Meta data can only be patched by owning user/session or the device itself", http.StatusBadRequest)
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var device Device
	err = collection.FindOne(ctx,
		bson.M{
			"_id":     deviceID,
			"garbage": bson.M{"$ne": true},
		}).
		Decode(&device)

	if err != nil {
		utils.RestErrorWrapper(w, "error finding device "+err.Error(), http.StatusBadRequest)
		return
	}
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

	deviceID := r.PathParam("id")
	if (authType != "USER" && authType != "SESSION") && !strings.HasSuffix(owner.(string), "/"+deviceID) {
		utils.RestErrorWrapper(w, "User data can only be updated by User or the device itself", http.StatusBadRequest)
		return
	}

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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	query := bson.M{
		"_id": deviceObjectID,
	}

	if authType != "DEVICE" {
		query["owner"] = owner.(string)
	}

	updateResult, err := collection.UpdateOne(
		ctx,
		query,
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
func (a *App) handleGetUserData(w rest.ResponseWriter, r *rest.Request) {
	var device Device

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	ownerPtr := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if ownerPtr == nil {
		ownerPtr = authID
	}

	owner, ok := ownerPtr.(string)
	if !ok {
		utils.RestErrorWrapper(w, "Session has no owner info", http.StatusBadRequest)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
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
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collectionAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collectionAccounts == nil {
		utils.RestErrorWrapper(w, "Error with Database (accounts) connectivity", http.StatusInternalServerError)
		return
	}

	value, useOtherOwnerPrn := r.URL.Query()["owner"]
	if useOtherOwnerPrn {
		ok, err := utils.ValidateUserPrn(value[0])
		if err != nil || !ok {
			utils.RestErrorWrapper(w, "Invalid owner prn", http.StatusForbidden)
			return
		}
		owner = value[0]
	}

	value, useOtherOwnerNick := r.URL.Query()["owner-nick"]
	if useOtherOwnerNick {
		account, err := a.GetUserAccountByNick(r.Context(), value[0])
		if err != nil {
			utils.RestErrorWrapper(w, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
			return
		}
		owner = account.Prn
	}

	mgoid, err := a.ResolveDeviceIDOrNick(r.Context(), owner, r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, query).Decode(&device)
	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	if authID != device.Prn && authID != device.Owner {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	if device.Owner != "" {
		var ownerAccount accounts.Account

		// first check default accounts like user1, user2, etc...
		ownerAccount, ok := accountsdata.DefaultAccounts[device.Owner]
		if !ok {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			err := collectionAccounts.FindOne(ctx,
				bson.M{"prn": device.Owner}).
				Decode(&ownerAccount)

			if err != nil {
				utils.RestErrorWrapper(w, "Owner account not Found", http.StatusInternalServerError)
				return
			}
		}

		profileMeta, _ := a.getProfileMetaData(r.Context(), device.Owner)
		device.UserMeta = utils.MergeMaps(profileMeta, device.UserMeta)
		device.OwnerNick = ownerAccount.Nick
	}

	w.WriteJson(utils.BsonUnquoteMap(&device.UserMeta))
}
