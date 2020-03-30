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
	"log"
	"net/http"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
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

	sha, err := utils.DecodeSha256HexString(putID)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageID := MakeStorageID(ownerStr, sha)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner != owner {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newObject)

	newObject.Owner = owner.(string)
	newObject.StorageID = storageID
	newObject.ID = putID

	SyncObjectSizes(&newObject)
	result, err := CalcUsageAfterPut(ownerStr, a.mongoClient, newObject.ID, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := a.GetDiskQuota(ownerStr)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		utils.RestErrorWrapper(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		utils.RestErrorWrapper(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": storageID},
		bson.M{"$set": newObject},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newObject)
}
