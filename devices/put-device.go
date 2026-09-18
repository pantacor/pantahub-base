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
func (a *App) handlePutDevice(c *echo.Context) error {

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
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	err = collection.FindOne(ctx,
		bson.M{"_id": deviceObjectID}).
		Decode(&newDevice)

	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	prn := newDevice.Prn
	timeCreated := newDevice.TimeCreated
	owner := newDevice.Owner
	challenge := newDevice.Challenge
	challengeVal := c.Request().FormValue("challenge")
	isPublic := newDevice.IsPublic
	userMeta := utils.BsonUnquoteMap(&newDevice.UserMeta)
	deviceMeta := utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	if callerIsDevice && newDevice.Prn != authID {
		return echoutil.RestErrorWrapper(c, "Not Device Accessible Resource Id", http.StatusForbidden)
	}

	if callerIsUser && newDevice.Owner != "" && newDevice.Owner != authID {
		return echoutil.RestErrorWrapper(c, "Not User Accessible Resource Id", http.StatusForbidden)
	}

	// Pantavisor registers and pvr claims with a body-less request, so an
	// empty payload is the same as "{}"; only malformed JSON is rejected.
	if err := echoutil.DecodeJsonPayload(c, &newDevice); err != nil && err != echoutil.ErrJsonPayloadEmpty {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	if newDevice.ID.Hex() != putID {
		return echoutil.RestErrorWrapper(c, "Cannot change device Id in PUT", http.StatusForbidden)
	}

	if newDevice.Prn != prn {
		return echoutil.RestErrorWrapper(c, "Cannot change device prn in PUT", http.StatusForbidden)
	}

	if newDevice.Owner != owner {
		return echoutil.RestErrorWrapper(c, "Cannot change device owner in PUT", http.StatusForbidden)
	}

	if newDevice.TimeCreated != timeCreated {
		return echoutil.RestErrorWrapper(c, "Cannot change device timeCreated in PUT", http.StatusForbidden)
	}

	// The secret is generated once at registration and can never be changed
	// through PUT; drop anything the client sent so it is neither stored nor
	// echoed back.
	newDevice.Secret = ""

	if callerIsDevice && newDevice.IsPublic != isPublic {
		return echoutil.RestErrorWrapper(c, "Device cannot change its own 'public' state", http.StatusForbidden)
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
				quotaResult, err := CheckDeviceQuota(c.Request().Context(), authID.(string), a.mongoClient, a.subService)
				if err != nil {
					return echoutil.RestErrorWrapper(c, "Error checking device quota: "+err.Error(), http.StatusInternalServerError)
				}
				if quotaResult.Exceeded {
					return echoutil.RestErrorWrapperUser(c, "device quota exceeded",
						"Device quota exceeded; delete some devices or request a quota bump from team@pantahub.com",
						http.StatusForbidden)
				}
			}

			newDevice.Owner = authID.(string)
			newDevice.Challenge = ""
			// if device had no proper nick, we assign one.
			if strings.HasPrefix(newDevice.Nick, "__unregistered__") {
				newDevice.Nick = GenerateDeviceNick()
			}
		} else {
			return echoutil.RestErrorWrapper(c, "No Access to Device", http.StatusForbidden)
		}
	}

	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
	}
	if !isValidNick {
		return echoutil.RestErrorWrapper(c, "Invalid Device Nick(Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
	}

	newDevice.TimeModified = time.Now()
	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// Build update document to avoid overwriting sensitive fields accidentally
	// and to ensure we don't clobber metadata we didn't intend to change.
	// The secret/secret_hash fields are deliberately absent: PUT never touches them.
	updateDoc := bson.M{
		"$set": bson.M{
			"nick":         newDevice.Nick,
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
		return echoutil.RestErrorWrapper(c, "error updating device: "+err.Error(), http.StatusBadRequest)
	}

	// unquote back to original format
	newDevice.UserMeta = utils.BsonUnquoteMap(&newDevice.UserMeta)
	newDevice.DeviceMeta = utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	return echoutil.WriteJSON(c, http.StatusOK, newDevice)
}
