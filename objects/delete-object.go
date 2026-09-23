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

package objects

import (
	"context"
	"net/http"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gopkg.in/mgo.v2/bson"
)

// handleDeleteObject Mark a object to be deleted
// @Summary Mark a object to be deleted
// @Description Mark a object to be deleted
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags objects
// @Param id path string true "Object ID"
// @Success 200 {object} Object
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /objects/{id} [delete]
func (a *App) handleDeleteObject(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in as a USER", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "Invalid Access", http.StatusForbidden)
	}

	delID := c.Param("id")
	sha, err := utils.DecodeSha256HexString(delID)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
	}
	storageID := MakeStorageID(ownerStr, sha)

	newObject := Object{}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if newObject.Owner == owner {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()
		deleteResult, err := collection.DeleteOne(ctx, bson.M{
			"_id":     storageID,
			"garbage": bson.M{"$ne": true},
		})
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
		}
		if deleteResult.DeletedCount == 0 {
			return echoutil.RestErrorWrapper(c, "Object not deleted", http.StatusForbidden)
		}
	}

	return echoutil.WriteJSON(c, http.StatusOK, newObject)
}
