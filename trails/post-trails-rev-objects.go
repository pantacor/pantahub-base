//
// Copyright (c) 2017-2023 Pantacor Ltd.
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

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handlePostStepsObject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := trailmodels.Step{}

	trailID := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType != "DEVICE" && authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Unknown AuthType", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).
		Decode(&step)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" && step.Device != owner {
		utils.RestErrorWrapper(w, "No access for device", http.StatusForbidden)
		return
	} else if (authType == "USER" || authType == "SESSION") && step.Owner != owner {
		utils.RestErrorWrapper(w, "No access for 'foreign' user/session", http.StatusForbidden)
		return
	}

	autoLink := true
	autolinkValue, ok := r.URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	newObject := objects.Object{}
	r.DecodeJsonPayload(&newObject)
	newObject.Owner = step.Owner

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	// check preconditions
	if newObject.Sha == "" {
		utils.RestErrorWrapper(w, "Post New Object must set a sha", http.StatusBadRequest)
		return
	}

	if newObject.ID == "" {
		newObject.ID = newObject.Sha
	}

	if newObject.ID != newObject.Sha {
		utils.RestErrorWrapper(w, "Post New Object must not have conflicting id and sha field", http.StatusBadRequest)
		return
	}

	shabyte, err := utils.DecodeSha256HexString(newObject.Sha)
	if err != nil {
		utils.RestErrorWrapper(w, "Object sha must be a valid sha256:"+err.Error(), http.StatusBadRequest)
		return
	}

	newObject.StorageID = objects.MakeStorageID(owner.(string), shabyte)

	objectsapp := objects.Build(a.mongoClient)

	resolvedObject, err := objectsapp.ResolveObjectWithBacking(r.Context(), owner.(string), newObject.Sha)

	if err != nil && err != objects.ErrNoBackingFile && err != mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "Error resolving Object "+err.Error(), http.StatusBadRequest)
		return
	}

	// if there was a backing file we have a conflict
	if resolvedObject != nil {
		w.Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeObject)
		w.WriteHeader(http.StatusConflict)
		newObject = *resolvedObject
		goto conflict
	}

	// here we had no backing file to link to and no object at all
	// we will try to create a link to an object available in a public step
	resolvedObject, err = objectsapp.ResolveObjectWithLinks(r.Context(), owner.(string), newObject.Sha, autoLink)

	// if this was possible, we use this object with adjusted Name from newObject
	// and store it in our object collection
	if err == nil {
		resolvedObject.ObjectName = newObject.ObjectName
		err = objectsapp.SaveObject(r.Context(), resolvedObject, false)
		if err != nil {
			utils.RestErrorWrapper(w, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
			return
		}
		// we have a gettable object in our db now so we conflict
		w.Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeLink)
		w.WriteHeader(http.StatusConflict)
		newObject = *resolvedObject
		goto conflict
	} else if err != objects.ErrNoLinkTargetAvail && err != mongo.ErrNoDocuments && err != objects.ErrNoBackingFile {
		utils.RestErrorWrapper(w, "Internal issue loading looking up object "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = objectsapp.SaveObject(r.Context(), &newObject, false)
	if err != nil {
		utils.RestErrorWrapper(w, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
		return
	}

conflict:
	newObjectWithAccess := objects.GetObjectWithAccess(newObject, "/trails")
	w.WriteJson(newObjectWithAccess)
}
