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
	"context"
	"net/http"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handleGetObject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
		// XXX: find right error
		if !ok {
			utils.RestErrorWrapper(w, "You need to be logged in as USER or DEVICE with owner", http.StatusForbidden)
			return
		}
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

	objID := r.PathParam("id")
	sha, err := utils.DecodeSha256HexString(objID)

	if err != nil {
		utils.RestErrorWrapper(w, "Get New Object :id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageID := MakeStorageID(ownerStr, sha)

	var filesObj Object
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&filesObj)

	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if filesObj.Owner != owner {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	issuerURL := utils.GetAPIEndpoint("/objects")
	filesObjWithAccess := MakeObjAccessible(issuerURL, ownerStr, filesObj, storageID)

	if filesObj.LinkedObject != "" {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeLink)
	} else {
		w.Header().Add(HttpHeaderPantahubObjectType, ObjectTypeObject)
	}

	w.WriteJson(filesObjWithAccess)
}
