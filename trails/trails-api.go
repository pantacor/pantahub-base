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
//
// ## Trail API Overview
//
// A trail represents a RESTful device state management endpoint optimized for
// high latency, asynchronous configuration management as found in the problem
// space of management of edge compute device world.
//
//	XXX: add proper API high level doc here (deleted outdated content)
//	     handler func inline doc should stay up to date though...
//
// Detailed documentation for the various operations on the API endpoints can be
// at the handler functions below.
//
// TODOs:
//   - properly document trails API once finalized
//   - abstract access control in a better managable manner and less
//     mistake/oversight likely manner (probably descriptive/configuration style)
//   - ensure step and progres time can be effectively read from trail meta info
//     probably async/delayed update to ensure scalability (e.g. once every 5
//     minute if there has been any step touch we update last progress etc
//   - ensure that devices can query steps that need enqueing efficiently
//   - enusre that in-sync time and status is timely updated based on step and
//     progress
//   - find smart way to figure when device is in sync based on reported state
//   - consider enforcing sequential processing of steps to have a clean tail?
package trails

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// App trails rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// XXX: no product without fixing this to only parse ids that belong to this
// service instance
func prnGetID(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1:]
}

func (a *App) getLatestStepRev(pctx context.Context, trailID primitive.ObjectID) (int, error) {
	collSteps := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collSteps == nil {
		return -1, errors.New("bad database connetivity")
	}

	steps := []trailmodels.Step{}
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()

	findOptions := options.Find()
	findOptions.SetSort(bson.M{"rev": -1})

	query := bson.M{
		"trail-id": trailID,
		"garbage":  bson.M{"$ne": true},
	}
	cursor, err := collSteps.Find(ctx, query, findOptions)
	if err != nil {
		return -1, err
	}

	err = cursor.All(ctx, &steps)
	if err != nil {
		return -1, err
	}

	if len(steps) == 0 {
		return -1, errors.New("no step found for trail: " + trailID.Hex())
	}

	return steps[0].Rev, err
}

func (a *App) handlePutStepsObject(w rest.ResponseWriter, r *rest.Request) {

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
	putID := r.PathParam("obj")

	if authType != "DEVICE" && authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Unknown AuthType", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).Decode(&step)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" && step.Device != owner {
		utils.RestErrorWrapper(w, "No access for device", http.StatusForbidden)
		return
	} else if (authType == "USER" || authType == "SESSION") && step.Owner != owner {
		utils.RestErrorWrapper(w, "No access for user/session", http.StatusForbidden)
		return
	}

	newObject := objects.Object{}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	sha, err := utils.DecodeSha256HexString(putID)

	if err != nil {
		utils.RestErrorWrapper(w, "Put Trails Steps Object id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageID := objects.MakeStorageID(step.Owner, sha)

	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner != step.Owner {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	nID := newObject.ID
	nOwner := newObject.Owner
	nStorageID := newObject.StorageID
	r.DecodeJsonPayload(&newObject)

	if newObject.ID != nID {
		utils.RestErrorWrapper(w, "Illegal Call Parameter Id", http.StatusConflict)
		return
	}
	if newObject.Owner != nOwner {
		utils.RestErrorWrapper(w, "Illegal Call Parameter Owner", http.StatusConflict)
		return
	}
	if newObject.StorageID != nStorageID {
		utils.RestErrorWrapper(w, "Illegal Call Parameter StorageId", http.StatusConflict)
		return
	}

	objects.SyncObjectSizes(&newObject)
	result, err := objects.CalcUsageAfterPut(r.Context(), newObject.Owner, a.mongoClient, newObject.ID, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := objects.GetDiskQuota(r.Context(), newObject.Owner)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		utils.RestErrorWrapper(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		utils.RestErrorWrapperUser(
			w,
			err.Error(),
			"Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": storageID},
		bson.M{"$set": newObject},
		updateOptions,
	)
	if updateResult.MatchedCount == 0 {
		w.WriteHeader(http.StatusConflict)
		w.Header().Add("X-PH-Error", "Error inserting object into database ")
	}
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		w.Header().Add("X-PH-Error", "Error inserting object into database "+err.Error())
	}

	issuerURL := utils.GetAPIEndpoint("/trails")
	newObjectWithAccess := objects.MakeObjAccessible(issuerURL, newObject.Owner, newObject, storageID)
	w.WriteJson(newObjectWithAccess)
}

// ProcessObjectsInState :
/*
1.Get Object List from the State field
2.UnMark All Objects As Garbages if they are marked as garbage
*/
func ProcessObjectsInState(
	pctx context.Context,
	owner string,
	state map[string]interface{},
	autoLink bool,
	a *App,
) (
	objects []string,
	err error,
) {
	objectList, err := GetStateObjects(pctx, owner, state, autoLink, a)
	if err != nil {
		return objectList, err
	}
	err = RestoreObjects(pctx, objectList, a)
	if err != nil {
		return objectList, err
	}
	return objectList, nil
}

// GetStateObjects : Get State Objects
func GetStateObjects(
	pctx context.Context,
	owner string,
	state map[string]interface{},
	autoLink bool,
	a *App,
) (
	[]string,
	error,
) {
	objectList := []string{}
	objMap := map[string]bool{}
	if len(state) == 0 {
		return objectList, nil
	}

	spec, ok := state["#spec"]
	if !ok {
		return nil, errors.New("state_object: Invalid state:#spec is missing")
	}

	specValue, ok := spec.(string)
	if !ok {
		return nil, errors.New("state_object: Invalid state:Value of #spec should be string")
	}

	if specValue != "pantavisor-multi-platform@1" && specValue != "pantavisor-service-embed@1" &&
		specValue != "pantavisor-service-system@1" {
		return nil, errors.New("state_object: Invalid state:Value of #spec should not be " + specValue)
	}

	objectsApp := objects.Build(a.mongoClient)

	for key, v := range state {
		if strings.HasSuffix(key, ".json") ||
			key == "#spec" {
			continue
		}
		sha, found := v.(string)
		if !found {
			statejson, err := json.Marshal(state)
			if err != nil {
				return nil, fmt.Errorf("state_object: state can not be parse to json -- %w", err)
			}
			return nil, fmt.Errorf("state_object: Object is not a string[%s: %s] \n state details: \n %s", key, sha, statejson)
		}

		ctx := context.WithoutCancel(pctx)
		object, err := objectsApp.ResolveObjectWithLinks(ctx, owner, sha, autoLink)

		if err != nil {
			return nil, err
		}

		// Save object
		ctx = context.WithoutCancel(pctx)
		err = objectsApp.SaveObject(ctx, object, false)
		if err != nil {
			return nil, errors.New("Error saving object: " + err.Error())
		}

		if _, ok := objMap[object.StorageID]; !ok {
			objectList = append(objectList, object.StorageID)
		}
	}
	return objectList, nil
}

// RestoreObjects : Takes the list of objects and unmarks them garbage.
func RestoreObjects(
	pctx context.Context,
	objectList []string,
	a *App,
) error {

	for _, storageSha := range objectList {

		ctx := context.WithoutCancel(pctx)
		result, err := IsObjectGarbage(ctx, storageSha, a)
		if err != nil {
			return errors.New("Error checking garbage object: " + err.Error() + "[sha:" + storageSha + "]")
		}
		if result {
			ctx := context.WithoutCancel(pctx)
			err := UnMarkObjectAsGarbage(ctx, storageSha, a)
			if err != nil {
				return errors.New("Error unmarking object as garbage: " + err.Error() + "[sha:" + storageSha + "]")
			}
		}
	}
	return nil
}

// IsObjectGarbage : to check if an object is garbage or not
func IsObjectGarbage(pctx context.Context, ObjectID string, a *App) (
	bool,
	error,
) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	objectCount, err := collection.CountDocuments(
		ctx,
		bson.M{
			"_id":     ObjectID,
			"garbage": true,
		},
	)
	if err != nil {
		return false, errors.New("Error Finding Object: " + err.Error())
	}
	return (objectCount == 1), nil
}

// UnMarkObjectAsGarbage : to unmark object as garbage
func UnMarkObjectAsGarbage(pctx context.Context, ObjectID string, a *App) error {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id": ObjectID,
		},
		bson.M{"$set": bson.M{
			"garbage": false,
		}},
	)
	if updateResult.MatchedCount == 0 {
		return errors.New("unmark_object_as_garbage:Error updating object: not found")
	}
	if err != nil {
		return errors.New("unmark_object_as_garbage:Error updating object:" + err.Error())
	}
	return nil
}

// IsDevicePublic checks if a device is public or not
func (a *App) IsDevicePublic(ctx context.Context, ID primitive.ObjectID) (bool, error) {

	devicesApp := devices.Build(a.mongoClient)
	device := devices.Device{}

	err := devicesApp.FindDeviceByID(ctx, ID, &device)
	if err != nil {
		return false, err
	}
	return device.IsPublic, nil
}
