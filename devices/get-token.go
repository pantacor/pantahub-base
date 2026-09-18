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

// handleGetToken Get a device token by ID
// @Summary Get a device token by ID
// @Description Get a device token by ID
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID"
// @Success 200 {object} utils.PantahubDevicesJoinToken
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens/{id} [get]
func (a *App) handleGetToken(c *echo.Context) error {

	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD", http.StatusBadRequest)
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
	}

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Can only be accessed by User or Session", http.StatusBadRequest)
	}

	tokenID := c.Param("id")
	tokenIDBson, err := primitive.ObjectIDFromHex(tokenID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid token ID format: "+err.Error(), http.StatusBadRequest)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	var result utils.PantahubDevicesJoinToken
	err = collection.FindOne(ctx, bson.M{
		"_id":   tokenIDBson,
		"owner": caller.(string),
	}).Decode(&result)

	if err != nil {
		if mongoutils.IsNotFound(err) {
			return echoutil.RestErrorWrapper(c, "Device token not found", http.StatusNotFound)
		}
		return echoutil.RestErrorWrapper(c, "Error getting device token: "+err.Error(), http.StatusInternalServerError)
	}

	// lets not reveal details about token when collection gets queried
	result.TokenSha = nil
	result.Token = ""

	return echoutil.WriteJSON(c, http.StatusOK, result)
}
