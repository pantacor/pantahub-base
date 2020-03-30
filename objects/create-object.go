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

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/storagedriver"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

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
	if authType.(string) == "USER" {
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
	var sha []byte
	var err error

	if newObject.Sha == "" {
		utils.RestErrorWrapper(w, "Post New Object must set a sha", http.StatusBadRequest)
		return
	}

	sha, err = utils.DecodeSha256HexString(newObject.Sha)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageID := MakeStorageID(ownerStr, sha)
	newObject.StorageID = storageID
	newObject.ID = newObject.Sha

	SyncObjectSizes(&newObject)

	result, err := CalcUsageAfterPost(ownerStr, a.mongoClient, newObject.ID, newObject.SizeInt)

	if err != nil {
		log.Printf("ERROR: CalcUsageAfterPost failed: %s\n", err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := a.GetDiskQuota(ownerStr)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {

		log.Println("Quota exceeded in post object.")
		utils.RestErrorWrapper(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	issuerURL := utils.GetAPIEndpoint("/objects")
	newObjectWithAccess := MakeObjAccessible(issuerURL, ownerStr, newObject, storageID)
	w.WriteJson(newObjectWithAccess)
}
