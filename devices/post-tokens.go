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
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	petname "github.com/dustinkirkland/golang-petname"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// handlePostTokens Create a new device token, used for authenticate as device
// @Summary Create a new device token, used for authenticate as device
// @Description Pantahub base offers a built in basic factory story in the sense that we offer the ability to auto assing devices to a specific owner.
// @Description For that right now we use a simple token based approach:
// @Description 1. Owner uses ```/devices/tokens/``` end point to create a new token; optionally he can also provide a set of default user-meta information that the auto assign feature will put in place for every device joinig using such token.
// @Description 2. Token is a one-time-visible secret that will only be displayed on reply of the token registration, but not afterwards. If user looses a token he can generate a new one. Old token can stay active if user does not believe the token has been compromised
// @Description 3. User configures device at factory to use the produced token as its pantahub registration credential. Pantavisor will then use the token when registering itself for first time. It uses ```Pantahub-Devices-Auto-Token-V1``` to pass the token to pantahub when registering itself. With this pantahub will auto assign the device to the owner of the given token and will put UserMeta in place.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {object} utils.PantahubDevicesJoinToken
// @Failure 400 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens [post]
func (a *App) handlePostTokens(w rest.ResponseWriter, r *rest.Request) {

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
		utils.RestErrorWrapper(w, "Can only be updated by User or Session: handle_posttoken", http.StatusBadRequest)
		return

	}

	req := utils.PantahubDevicesJoinToken{}

	err := r.DecodeJsonPayload(&req)

	if err != nil && err != rest.ErrJsonPayloadEmpty {
		utils.RestErrorWrapper(w, "error decoding request: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.ID = primitive.NewObjectID()
	req.Prn = utils.IDGetPrn(req.ID, "devices-tokens")

	if req.DefaultUserMeta != nil {
		req.DefaultUserMeta = utils.BsonQuoteMap(&req.DefaultUserMeta)
	}
	if req.Nick == "" {
		req.Nick = petname.Generate(3, "_")
	}

	req.Owner = caller.(string)

	key := make([]byte, 24)

	_, err = rand.Read(key)
	if err != nil {
		utils.RestErrorWrapper(w, "error generating random token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// calc sha for secret to store in DB
	shaSummer := sha256.New()
	_, err = shaSummer.Write(key)
	sum := make([]byte, shaSummer.Size())
	req.TokenSha = shaSummer.Sum(sum)

	// set timecreated/modified to NOW
	req.TimeCreated = time.Now()
	req.TimeModified = req.TimeCreated

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(ctx, &req)

	if err != nil {
		utils.RestErrorWrapper(w, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// do not return TokenSha, return encoded key
	req.Token = base64.StdEncoding.EncodeToString(key)
	req.TokenSha = nil

	w.WriteJson(&req)
}
