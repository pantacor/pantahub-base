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

package objects

import (
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// handlePutObject Update a object content
// @Summary Update a object content
// @Description Update a object content
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags objects
// @Param id path string true "Object ID"
// @Param body body string Object "Object payload"
// @Success 200 {object} Object
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /objects/{id} [put]
func (a *App) handlePutObject(c *echo.Context) error {

	newObject := Object{}

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
	putID := c.Param("id")

	// first validate that its a valid sha in hex format
	_, err := utils.DecodeSha256HexString(putID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
	}

	// now parse autolink flag from url query
	autoLink := true
	autolinkValue, ok := c.Request().URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	// find object owned by caller, but only if download is possible
	object, err := a.ResolveObjectWithBacking(c.Request().Context(), ownerStr, putID)

	if err != nil && ErrNoBackingFile != err {
		return echoutil.RestErrorWrapper(c, "Object to update not found", http.StatusBadRequest)
	}

	if ErrNoBackingFile == err {
		object, err = a.ResolveObjectWithLinks(c.Request().Context(), ownerStr, putID, autoLink)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "No link found for object without backing file", http.StatusBadRequest)
		}
	}

	if object == nil {
		object, err = a.ResolveObjectWithLinks(c.Request().Context(), ownerStr, putID, autoLink)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "No link found for not existing object", http.StatusBadRequest)
		}
	}

	// here we have a downloadable object; make sure owner is calling...
	if object.Owner != owner {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	// parse object structure from input
	err = echoutil.DecodeJsonPayload(c, &newObject)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Cannot decode request: "+err.Error(), http.StatusBadRequest)
	}

	// here we have a downloadable object; owner must match input
	if newObject.Owner != object.Owner {
		return echoutil.RestErrorWrapper(c, "Cannot modify object owner", http.StatusBadRequest)
	}

	// sha must match
	if newObject.Sha != object.Sha {
		return echoutil.RestErrorWrapper(c, "Cannot modify object sha", http.StatusBadRequest)
	}

	if newObject.ObjectName != "" {
		object.ObjectName = newObject.ObjectName
	}

	err = a.SaveObject(c.Request().Context(), object, false)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to save object: "+err.Error(), http.StatusInternalServerError)
	}

	if object.LinkedObject != "" {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
	} else {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
	}

	return echoutil.WriteJSON(c, http.StatusOK, object)
}
