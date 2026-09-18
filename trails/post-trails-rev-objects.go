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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"net/http"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// handlePostStepsObject Create a new object for a trail revision
// @Summary Create a new object for a trail revision
// @Description Create a new object for a trail revision
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Param body body objects.Object true "Object payload"
// @Success 200 {object} objects.ObjectWithAccess
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/objects [post]
func (a *App) handlePostStepsObject(c *echo.Context) error {

	status := http.StatusOK
	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	step := trailmodels.Step{}

	trailID := c.Param("id")
	rev := c.Param("rev")

	if authType != "DEVICE" && authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Unknown AuthType", http.StatusBadRequest)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).
		Decode(&step)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if authType == "DEVICE" && step.Device != owner {
		return echoutil.RestErrorWrapper(c, "No access for device", http.StatusForbidden)
	} else if (authType == "USER" || authType == "SESSION") && step.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No access for 'foreign' user/session", http.StatusForbidden)
	}

	autoLink := true
	autolinkValue, ok := c.Request().URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	newObject := objects.Object{}
	if err := echoutil.DecodeJsonPayload(c, &newObject); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}
	newObject.Owner = step.Owner

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

	shabyte, err := utils.DecodeSha256HexString(newObject.Sha)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Object sha must be a valid sha256:"+err.Error(), http.StatusBadRequest)
	}

	newObject.StorageID = objects.MakeStorageID(owner.(string), shabyte)

	objectsapp := objects.Build(a.mongoClient)

	resolvedObject, err := objectsapp.ResolveObjectWithBacking(c.Request().Context(), owner.(string), newObject.Sha)

	if err != nil && err != objects.ErrNoBackingFile && err != mongo.ErrNoDocuments {
		return echoutil.RestErrorWrapper(c, "Error resolving Object "+err.Error(), http.StatusBadRequest)
	}

	// if there was a backing file we have a conflict
	if resolvedObject != nil {
		c.Response().Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeObject)
		status = http.StatusConflict
		newObject = *resolvedObject
		goto conflict
	}

	// here we had no backing file to link to and no object at all
	// we will try to create a link to an object available in a public step
	resolvedObject, err = objectsapp.ResolveObjectWithLinks(c.Request().Context(), owner.(string), newObject.Sha, autoLink)

	// if this was possible, we use this object with adjusted Name from newObject
	// and store it in our object collection
	if err == nil {
		resolvedObject.ObjectName = newObject.ObjectName
		err = objectsapp.SaveObject(c.Request().Context(), resolvedObject, false)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
		}
		// we have a gettable object in our db now so we conflict
		c.Response().Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeLink)
		status = http.StatusConflict
		newObject = *resolvedObject
		goto conflict
	} else if err != objects.ErrNoLinkTargetAvail && err != mongo.ErrNoDocuments && err != objects.ErrNoBackingFile {
		return echoutil.RestErrorWrapper(c, "Internal issue loading looking up object "+err.Error(), http.StatusInternalServerError)
	}

	err = objectsapp.SaveObject(c.Request().Context(), &newObject, false)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
	}

conflict:
	newObjectWithAccess := objects.GetObjectWithAccess(newObject, "/trails")
	return echoutil.WriteJSON(c, status, newObjectWithAccess)
}
