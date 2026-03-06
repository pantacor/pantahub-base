//
// Copyright 2025  Pantacor Ltd.
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
	"fmt"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// patchDeviceTokenRequest defines the fields that can be updated for a device token.
type patchDeviceTokenRequest struct {
	Nick            string                  `json:"nick,omitempty"`
	OVMode          *models.OVModeExtension `json:"ovmode,omitempty"`
	DefaultUserMeta map[string]interface{}  `json:"default-user-meta,omitempty"`
}

// handlePatchTokens Update a device token (Nick and OVMode only)
// @Summary Update a device token (Nick and OVMode only)
// @Description Update specific fields (Nick, OVMode) of a device token.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID of the token to update"
// @Param tokenBody body patchDeviceTokenRequest true "Fields to update"
// @Success 200 {object} utils.PantahubDevicesJoinToken
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens/{id} [patch]
func (a *App) handlePatchTokens(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Can not be updated by Device", http.StatusBadRequest)
		return
	}

	tokenID := r.PathParam("id")
	tokenIDBson, err := primitive.ObjectIDFromHex(tokenID)
	if err != nil {
		message := fmt.Sprintf("Invalid token ID format: %s", err.Error())
		utils.RestErrorWrapper(w, message, http.StatusBadRequest)
		return
	}

	// Parse request body
	patchReq := patchDeviceTokenRequest{}
	err = r.DecodeJsonPayload(&patchReq)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	updateFields := bson.M{}
	if patchReq.Nick != "" {
		updateFields["nick"] = patchReq.Nick
	}
	if patchReq.OVMode != nil {
		updateFields["ovmode"] = patchReq.OVMode
	}
	if patchReq.DefaultUserMeta != nil {
		update := map[string]interface{}{}
		for key, val := range patchReq.DefaultUserMeta {
			update[key] = val
		}
		updateFields["default-user-meta"] = update
	}

	if len(updateFields) == 0 {
		utils.RestErrorWrapper(w, "No updatable fields provided in request body (nick or ovmode)", http.StatusBadRequest)
		return
	}

	updateFields["time-modified"] = time.Now()

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"_id":   tokenIDBson,
		"owner": caller.(string),
	}

	update := bson.M{"$set": updateFields}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedToken utils.PantahubDevicesJoinToken
	err = collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedToken)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RestErrorWrapper(w, "Device token not found or not owned by caller", http.StatusNotFound)
		} else {
			utils.RestErrorWrapper(w, "Error updating device token: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	updatedToken.Token = ""
	updatedToken.TokenSha = []byte("")

	w.WriteJson(updatedToken)
}
