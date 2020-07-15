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

package objects

import (
	"errors"
	"net/http"

	jwtgo "github.com/dgrijalva/jwt-go"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handlePostObject(w rest.ResponseWriter, r *rest.Request) {

	newObject := Object{}

	r.DecodeJsonPayload(&newObject)

	var ownerStr string

	caller, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}
	callerStr, ok := caller.(string)

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if authType.(string) == "USER" || authType.(string) == "SESSION" {
		ownerStr = callerStr
	} else {
		owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
		if !ok {
			// XXX: find right error
			utils.RestErrorWrapper(w, "You need to be logged in as a USER or DEVICE", http.StatusForbidden)
			return
		}
		ownerStr = owner.(string)
	}

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access Token", http.StatusForbidden)
		return
	}

	newObject.Owner = ownerStr

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

	autoLink := true
	autolinkValue, ok := r.URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	shabyte, err := utils.DecodeSha256HexString(newObject.Sha)
	if err != nil {
		utils.RestErrorWrapper(w, "Object sha must be a valid sha256:"+err.Error(), http.StatusBadRequest)
		return
	}
	newObject.StorageID = MakeStorageID(ownerStr, shabyte)

	resolvedObject, err := a.ResolveObjectWithBacking(ownerStr, newObject.Sha)

	if err != nil && err != ErrNoBackingFile && err != mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "Error resolving Object "+err.Error(), http.StatusBadRequest)
		return
	}

	// if there was a backing file we have conflict
	if resolvedObject != nil {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
		w.WriteHeader(http.StatusConflict)
		newObject = *resolvedObject
		goto conflict
	}

	// here we had no backing file to link to and no object at all
	// we will try to create a link to an object available in a public step
	resolvedObject, err = a.ResolveObjectWithLinks(ownerStr, newObject.Sha, autoLink)

	// if this was possible, we use this object with adjusted Name from newObject
	// and store it in our object collection
	if err == nil {
		resolvedObject.ObjectName = newObject.ObjectName
		err = a.SaveObject(resolvedObject, false)
		if err != nil {
			utils.RestErrorWrapper(w, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
			return
		}
		// we have a gettable object in our db now so we conflict
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
		w.WriteHeader(http.StatusConflict)
		newObject = *resolvedObject
		goto conflict
	} else if err != ErrNoLinkTargetAvail && err != mongo.ErrNoDocuments && err != ErrNoBackingFile {
		utils.RestErrorWrapper(w, "Internal issue loading looking up object "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = a.SaveObject(&newObject, false)
	if err != nil {
		if utils.IsUserError(err) {
			utils.RestErrorWrapperUser(w, err.Error(), http.StatusInternalServerError)
		} else {
			utils.RestErrorWrapper(w, "Error saving our linkified object "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if newObject.LinkedObject != "" {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
	} else {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
	}

conflict:
	newObjectWithAccess := GetObjectWithAccess(newObject, "/objects")
	w.WriteJson(&newObjectWithAccess)
}
