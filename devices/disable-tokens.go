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
	"fmt"
	"net/http"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// handleDisableTokens Disable a device token in order to be unable to used as authetication
// @Summary Disable a device token in order to be unable to used as authetication
// @Description Disable a device token in order to be unable to used as authetication
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|Nick|PRN"
// @Success 200 {object} disableToken
// @Failure 400 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens/{id} [delete]
func (a *App) handleDisableTokens(c *echo.Context) error {

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
		return echoutil.RestErrorWrapper(c, "Can not be updated by Device: handle_posttoken", http.StatusBadRequest)
	}

	_ = c.Request().ParseForm()
	tokenID := c.Param("id")
	tokenIDBson, err := primitive.ObjectIDFromHex(tokenID)
	if err != nil {
		message := fmt.Sprintf("error decoding id to ObjectID: %s -- %s", tokenID, err.Error())
		return echoutil.RestErrorWrapper(c, message, http.StatusInternalServerError)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   tokenIDBson,
			"owner": caller.(string),
		},
		bson.M{"$set": bson.M{"disabled": true}},
		updateOptions,
	)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, bson.M{"status": "OK"})
}
