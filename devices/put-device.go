//
// Copyright 2026 Pantacor Ltd.
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

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

type challengePayload struct {
	Challenge string `json:"challenge"`
}

// handlePutDevice Claim a device by resolving challenge
// @Summary Claim a device by resolving challenge
// @Description  Claim a device as a logged in user with TOKEN
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body challengePayload true "Device payload"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id} [put]
func (a *App) handlePutDevice(w rest.ResponseWriter, r *rest.Request) {

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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx,
		bson.M{"_id": deviceObjectID}).
		Decode(&newDevice)

	if err != nil && mongoutils.IsNotFound(err) {
		utils.RestErrorWrapper(w, "Device not found", http.StatusNotFound)
		return
	}

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	prn := newDevice.Prn
	timeCreated := newDevice.TimeCreated
	owner := newDevice.Owner
	challenge := newDevice.Challenge
	challengeVal := r.FormValue("challenge")
	isPublic := newDevice.IsPublic
	userMeta := utils.BsonUnquoteMap(&newDevice.UserMeta)
	deviceMeta := utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	if callerIsDevice && newDevice.Prn != authID {
		utils.RestErrorWrapper(w, "Not Device Accessible Resource Id", http.StatusForbidden)
		return
	}

	if callerIsUser && newDevice.Owner != "" && newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newDevice)

	if newDevice.ID.Hex() != putID {
		utils.RestErrorWrapper(w, "Cannot change device Id in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Prn != prn {
		utils.RestErrorWrapper(w, "Cannot change device prn in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Owner != owner {
		utils.RestErrorWrapper(w, "Cannot change device owner in PUT", http.StatusForbidden)
		return
	}

	if newDevice.TimeCreated != timeCreated {
		utils.RestErrorWrapper(w, "Cannot change device timeCreated in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Secret == "" {
		utils.RestErrorWrapper(w, "Empty Secret not allowed for devices in PUT", http.StatusForbidden)
		return
	}

	if callerIsDevice && newDevice.IsPublic != isPublic {
		utils.RestErrorWrapper(w, "Device cannot change its own 'public' state", http.StatusForbidden)
		return
	}

	// if device puts info, always reset the user part of the data and vv.
	if callerIsDevice {
		newDevice.UserMeta = utils.BsonQuoteMap(&userMeta)
	} else {
		newDevice.DeviceMeta = utils.BsonQuoteMap(&deviceMeta)
	}

	/* in case someone claims the device like this, update owner */
	if len(challenge) > 0 {
		if challenge == challengeVal {
			// Check device quota before claiming device if it's currently unowned
			if owner == "" {
				quotaResult, err := CheckDeviceQuota(r.Context(), authID.(string), a.mongoClient, a.subService)
				if err != nil {
					utils.RestErrorWrapper(w, "Error checking device quota: "+err.Error(), http.StatusInternalServerError)
					return
				}
				if quotaResult.Exceeded {
					utils.RestErrorWrapperUser(w, "device quota exceeded",
						"Device quota exceeded; delete some devices or request a quota bump from team@pantahub.com",
						http.StatusForbidden)
					return
				}
			}

			newDevice.Owner = authID.(string)
			newDevice.Challenge = ""
			// if device had no proper nick, we assign one.
			if strings.HasPrefix(newDevice.Nick, "__unregistered__") {
				newDevice.Nick = GenerateDeviceNick()
			}
		} else {
			utils.RestErrorWrapper(w, "No Access to Device", http.StatusForbidden)
			return
		}
	}

	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isValidNick {
		utils.RestErrorWrapper(w, "Invalid Device Nick(Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
		return
	}

	newDevice.TimeModified = time.Now()
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Build update document to avoid overwriting sensitive fields accidentally
	// and to ensure we don't clobber metadata we didn't intend to change.
	updateDoc := bson.M{
		// a supplied secret replaces any legacy plaintext one with its hash
		"$unset": bson.M{"secret": ""},
		"$set": bson.M{
			"nick":         newDevice.Nick,
			"secret_hash":  utils.HashSecret(newDevice.Secret),
			"ispublic":     newDevice.IsPublic,
			"timemodified": newDevice.TimeModified,
			"challenge":    newDevice.Challenge,
			"owner":        newDevice.Owner,
		},
	}

	// Only update metadata if we are the authorized party for that metadata
	if callerIsDevice {
		updateDoc["$set"].(bson.M)["device-meta"] = newDevice.DeviceMeta
	} else {
		updateDoc["$set"].(bson.M)["user-meta"] = newDevice.UserMeta
	}

	if newDevice.IsPublic != isPublic {
		// clear the flag so the kafka listener re-syncs steps
		updateDoc["$set"].(bson.M)["mark_public_processed"] = false
	}

	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		updateDoc,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "error updating device: "+err.Error(), http.StatusBadRequest)
		return
	}

	// unquote back to original format
	newDevice.UserMeta = utils.BsonUnquoteMap(&newDevice.UserMeta)
	newDevice.DeviceMeta = utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	w.WriteJson(newDevice)
}
