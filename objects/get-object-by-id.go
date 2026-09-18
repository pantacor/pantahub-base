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

// handleGetObject Retrive and object by ID
// @Summary Retrive and object by ID
// @Description Retrive and object by ID
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags objects
// @Param id path string true "Object ID"
// @Success 200 {object} Object
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /objects/{id} [get]
func (a *App) handleGetObject(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["owner"]
	if !ok {
		owner, ok = c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
		// XXX: find right error
		if !ok {
			return echoutil.RestErrorWrapper(c, "You need to be logged in as USER or DEVICE with owner", http.StatusForbidden)
		}
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

	objID := c.Param("id")
	sha, err := utils.DecodeSha256HexString(objID)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Get New Object :id must be a valid sha256", http.StatusBadRequest)
	}

	storageID := MakeStorageID(ownerStr, sha)

	var filesObj Object
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&filesObj)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if filesObj.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	issuerURL := utils.GetAPIEndpoint("/objects")
	filesObjWithAccess := MakeObjAccessible(issuerURL, ownerStr, filesObj, storageID)

	if filesObj.LinkedObject != "" {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
	} else {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
	}

	return echoutil.WriteJSON(c, http.StatusOK, filesObjWithAccess)
}
