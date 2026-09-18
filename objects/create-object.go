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
	"context"
	"errors"
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// ErrObjectS3PathAlreadyExists  erro variable for "local s3 file path for object is already exists"
var ErrObjectS3PathAlreadyExists error = errors.New("local s3 file path for object is already exists")

// handlePostObject Create a new object for a owner token
// @Summary Create a new object for a owner token
// @Description Create a new object for a owner token
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags objects
// @Param body body Object true "Object payload"
// @Success 200 {object} ObjectWithAccess
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /objects [post]
func (a *App) handlePostObject(c *echo.Context) error {

	newObject := Object{}
	status := http.StatusOK

	if err := echoutil.DecodeJsonPayload(c, &newObject); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	var ownerStr string

	caller, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}
	callerStr, ok := caller.(string)

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if authType.(string) == "USER" || authType.(string) == "SESSION" {
		ownerStr = callerStr
	} else {
		owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["owner"]
		if !ok {
			// XXX: find right error
			return echoutil.RestErrorWrapper(c, "You need to be logged in as a USER or DEVICE", http.StatusForbidden)
		}
		ownerStr = owner.(string)
	}

	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "Invalid Access Token", http.StatusForbidden)
	}

	newObject.Owner = ownerStr

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	// check preconditions
	if newObject.Sha == "" {
		return echoutil.RestErrorWrapper(c, "Post New Object must set a sha", http.StatusBadRequest)
	}

	if newObject.ID == "" {
		newObject.ID = newObject.Sha
	}

	if newObject.ID != newObject.Sha {
		return echoutil.RestErrorWrapper(c, "Post New Object must not have conflicting id and sha field", http.StatusBadRequest)
	}

	autoLink := true
	autolinkValue, ok := c.Request().URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	shabyte, err := utils.DecodeSha256HexString(newObject.Sha)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Object sha must be a valid sha256:"+err.Error(), http.StatusBadRequest)
	}
	newObject.StorageID = MakeStorageID(ownerStr, shabyte)

	childCtx := context.WithoutCancel(c.Request().Context())
	resolvedObject, err := a.ResolveObjectWithBacking(childCtx, ownerStr, newObject.Sha)

	if err != nil && err != ErrNoBackingFile && err != mongo.ErrNoDocuments {
		return echoutil.RestErrorWrapper(c, "Error resolving Object "+err.Error(), http.StatusBadRequest)
	}

	// if there was a backing file we have conflict
	if resolvedObject != nil {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
		status = http.StatusConflict
		newObject = *resolvedObject
		goto conflict
	}

	// here we had no backing file to link to and no object at all
	// we will try to create a link to an object available in a public step
	childCtx = context.WithoutCancel(c.Request().Context())
	resolvedObject, err = a.ResolveObjectWithLinks(childCtx, ownerStr, newObject.Sha, autoLink)

	// if this was possible, we use this object with adjusted Name from newObject
	// and store it in our object collection
	if err == nil {
		resolvedObject.ObjectName = newObject.ObjectName
		childCtx = context.WithoutCancel(c.Request().Context())
		err = a.SaveObject(childCtx, resolvedObject, false)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
		}
		// we have a gettable object in our db now so we conflict
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
		status = http.StatusConflict
		newObject = *resolvedObject
		goto conflict
	} else if err != ErrNoLinkTargetAvail && err != mongo.ErrNoDocuments && err != ErrNoBackingFile {
		return echoutil.RestErrorWrapper(c, "Internal issue loading looking up object "+err.Error(), http.StatusInternalServerError)
	}

	childCtx = context.WithoutCancel(c.Request().Context())
	err = a.SaveObject(childCtx, &newObject, false)
	if err != nil {
		if utils.IsUserError(err) {
			return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusInternalServerError)
		}
		return echoutil.RestErrorWrapper(c, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
	}
	if newObject.LinkedObject != "" {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
	} else {
		c.Response().Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
	}

conflict:
	newObjectWithAccess := GetObjectWithAccess(newObject, "/objects")
	return echoutil.WriteJSON(c, status, &newObjectWithAccess)
}
