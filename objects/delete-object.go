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
func (a *App) handleDeleteObject(w rest.ResponseWriter, r *rest.Request) {

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

	delID := r.PathParam("id")
	sha, err := utils.DecodeSha256HexString(delID)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}
	storageID := MakeStorageID(ownerStr, sha)

	newObject := Object{}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner == owner {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		deleteResult, err := collection.DeleteOne(ctx, bson.M{
			"_id":     storageID,
			"garbage": bson.M{"$ne": true},
		})
		if err != nil {
			utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
			return
		}
		if deleteResult.DeletedCount == 0 {
			utils.RestErrorWrapper(w, "Object not deleted", http.StatusForbidden)
			return
		}
	}

	w.WriteJson(newObject)
}
