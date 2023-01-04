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
	"log"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

// handleGetUserDevice get device by owner nick and device nick
// @Summary get device by owner nick and device nick
// @Description get device by owner nick and device nick
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param usernick path string true "NICK"
// @Param devicenick path string true "NICK"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/np/{usernick}/{devicenick} [get]
func (a *App) handleGetUserDevice(w rest.ResponseWriter, r *rest.Request) {

	var device Device
	var account accounts.Account

	usernick := r.PathParam("usernick")
	devicenick := r.PathParam("devicenick")

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
	} else if authType == "USER" || authType == "SESSION" {
		callerIsUser = true
	} else {
		utils.RestErrorWrapper(w, "You need to be logged in with either USER or DEVICE account type.", http.StatusForbidden)
		return
	}

	// first check if we refer to a default accoutn
	isDefaultAccount := false
	for _, v := range accountsdata.DefaultAccounts {
		if v.Nick == usernick {
			account = v
			isDefaultAccount = true
			break
		}
	}

	// if not a default, lets look for proper accounts in db...
	if !isDefaultAccount {

		collAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
		if collAccounts == nil {
			utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		err := collAccounts.FindOne(ctx,
			bson.M{"nick": usernick}).
			Decode(&account)

		if err != nil {
			log.Println("ERROR: error getting account by nick; will return Forbidden to cover up details from backend: " + err.Error())
			utils.RestErrorWrapper(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	collDevices := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collDevices == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err := collDevices.FindOne(ctx, bson.M{
		"owner":   account.Prn,
		"nick":    devicenick,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)

	if err != nil {
		log.Println("ERROR: error getting device by nick: " + err.Error())
		utils.RestErrorWrapper(w, "Forbidden", http.StatusForbidden)
		return
	}

	if !device.IsPublic {
		// XXX: fixme; needs delegation of authorization for device accessing its resources
		// could be subscriptions, but also something else
		if callerIsDevice && device.Prn != authID {
			utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
			return
		}

		if callerIsUser && device.Owner != authID {
			utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
			return
		}
	} else if !callerIsDevice && !callerIsUser {
		device.Challenge = ""
	}

	// we always hide the secret
	device.Secret = ""
	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	w.WriteJson(device)
}
