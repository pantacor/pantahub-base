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
	"net/http"

	jwtgo "github.com/dgrijalva/jwt-go"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handlePutObject(w rest.ResponseWriter, r *rest.Request) {

	newObject := Object{}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
		return
	}
	putID := r.PathParam("id")

	// first validate that its a valid sha in hex format
	_, err := utils.DecodeSha256HexString(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}

	// now parse autolink flag from url query
	autoLink := true
	autolinkValue, ok := r.URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	// find object owned by caller, but only if download is possible
	object, err := a.ResolveObjectWithBacking(r.Context(), ownerStr, putID)

	if err != nil && ErrNoBackingFile != err {
		utils.RestErrorWrapper(w, "Object to update not found", http.StatusBadRequest)
		return
	}

	if ErrNoBackingFile == err {
		object, err = a.ResolveObjectWithLinks(r.Context(), ownerStr, putID, autoLink)
		if err != nil {
			utils.RestErrorWrapper(w, "No link found for object without backing file", http.StatusBadRequest)
			return
		}
	}

	if object == nil {
		object, err = a.ResolveObjectWithLinks(r.Context(), ownerStr, putID, autoLink)
		if err != nil {
			utils.RestErrorWrapper(w, "No link found for not existing object", http.StatusBadRequest)
			return
		}
	}

	// here we have a downloadable object; make sure owner is calling...
	if object.Owner != owner {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	// parse object structure from input
	err = r.DecodeJsonPayload(&newObject)
	if err != nil {
		utils.RestErrorWrapper(w, "Cannot decode request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// here we have a downloadable object; owner must match input
	if newObject.Owner != object.Owner {
		utils.RestErrorWrapper(w, "Cannot modify object owner", http.StatusBadRequest)
		return
	}

	// sha must match
	if newObject.Sha != object.Sha {
		utils.RestErrorWrapper(w, "Cannot modify object sha", http.StatusBadRequest)
		return
	}

	if newObject.ObjectName != "" {
		object.ObjectName = newObject.ObjectName
	}

	err = a.SaveObject(r.Context(), object, false)
	if err != nil {
		utils.RestErrorWrapper(w, "Failed to save object: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if object.LinkedObject != "" {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
	} else {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
	}

	w.WriteJson(object)
}
