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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"log"
	"net/http"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/storagedriver"
	"gitlab.com/pantacor/pantahub-base/utils"
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

	step := Step{}

	trailID := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType != "DEVICE" && authType != "USER" {
		utils.RestErrorWrapper(w, "Unknown AuthType", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	} else if authType == "USER" && step.Owner != owner {
		utils.RestErrorWrapper(w, "No access for user", http.StatusForbidden)
		return
	}

	newObject := objects.Object{}
	r.DecodeJsonPayload(&newObject)

	newObject.Owner = step.Owner

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	sha, err := utils.DecodeSha256HexString(newObject.Sha)

	if err != nil {
		utils.RestErrorWrapper(w, "Post Steps Object id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageID := objects.MakeStorageID(newObject.Owner, sha)
	newObject.StorageID = storageID
	newObject.ID = newObject.Sha

	objects.SyncObjectSizes(&newObject)

	result, err := objects.CalcUsageAfterPost(newObject.Owner, a.mongoClient, newObject.ID, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := objects.GetDiskQuota(newObject.Owner)

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
	_, err = collection.InsertOne(
		ctx,
		newObject,
	)

	if err != nil {
		filePath, err := utils.MakeLocalS3PathForName(storageID)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error Finding Path for Name"+err.Error())
			return
		}

		sd := storagedriver.FromEnv()
		if sd.Exists(filePath) {
			w.WriteHeader(http.StatusConflict)
			w.Header().Add("X-PH-Error", "Cannot insert existing object into database")
			goto conflict
		}

		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		updatedResult, err := collection.UpdateOne(
			ctx,
			bson.M{"_id": newObject.StorageID},
			bson.M{"$set": newObject},
		)
		if updatedResult.MatchedCount == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error updating previously failed object in database ")
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error updating previously failed object in database "+err.Error())
			return
		}
		// we return anyway with the already available info about this object
	}
conflict:
	issuerURL := utils.GetAPIEndpoint("/trails")
	newObjectWithAccess := objects.MakeObjAccessible(issuerURL, newObject.Owner, newObject, storageID)
	w.WriteJson(newObjectWithAccess)
}
