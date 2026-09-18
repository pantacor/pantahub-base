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

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	jwtauth "gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// App trails rest application
type App struct {
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
}

func handleAuth(c *echo.Context) error {
	jwtClaims := c.Get(echoutil.KeyJWTPayload)
	return echoutil.WriteJSON(c, http.StatusOK, jwtClaims)
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

func (a *App) handlePutStepsObject(c *echo.Context) error {

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
	putID := c.Param("obj")

	if authType != "DEVICE" && authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Unknown AuthType", http.StatusBadRequest)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).Decode(&step)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if authType == "DEVICE" && step.Device != owner {
		return echoutil.RestErrorWrapper(c, "No access for device", http.StatusForbidden)
	} else if (authType == "USER" || authType == "SESSION") && step.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No access for user/session", http.StatusForbidden)
	}

	newObject := objects.Object{}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	sha, err := utils.DecodeSha256HexString(putID)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Put Trails Steps Object id must be a valid sha256", http.StatusBadRequest)
	}

	storageID := objects.MakeStorageID(step.Owner, sha)

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	if newObject.Owner != step.Owner {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	nID := newObject.ID
	nOwner := newObject.Owner
	nStorageID := newObject.StorageID
	if err := echoutil.DecodeJsonPayload(c, &newObject); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	if newObject.ID != nID {
		return echoutil.RestErrorWrapper(c, "Illegal Call Parameter Id", http.StatusConflict)
	}
	if newObject.Owner != nOwner {
		return echoutil.RestErrorWrapper(c, "Illegal Call Parameter Owner", http.StatusConflict)
	}
	if newObject.StorageID != nStorageID {
		return echoutil.RestErrorWrapper(c, "Illegal Call Parameter StorageId", http.StatusConflict)
	}

	objects.SyncObjectSizes(&newObject)
	result, err := objects.CalcUsageAfterPut(c.Request().Context(), newObject.Owner, a.mongoClient, newObject.ID, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		return echoutil.RestErrorWrapper(c, "Error posting object", http.StatusInternalServerError)
	}

	quota, err := objects.GetDiskQuota(c.Request().Context(), newObject.Owner)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		return echoutil.RestErrorWrapper(c, "Error to calc quota", http.StatusInternalServerError)
	}

	if result.Total > quota {
		return echoutil.RestErrorWrapperUser(
			c,
			"quota exceeded",
			"Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": storageID},
		bson.M{"$set": newObject},
		updateOptions,
	)
	if err != nil {
		c.Response().Header().Add("X-PH-Error", "Error inserting object into database "+err.Error())
		return echoutil.WriteHeader(c, http.StatusConflict)
	}
	if updateResult.MatchedCount == 0 && updateResult.UpsertedCount == 0 {
		c.Response().Header().Add("X-PH-Error", "Error inserting object into database ")
		return echoutil.WriteHeader(c, http.StatusConflict)
	}

	issuerURL := utils.GetAPIEndpoint("/trails")
	newObjectWithAccess := objects.MakeObjAccessible(issuerURL, newObject.Owner, newObject, storageID)
	return echoutil.WriteJSON(c, http.StatusOK, newObjectWithAccess)
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
			// A state routinely holds hundreds of objects, so returning the
			// bare sentinel leaves no way to tell which one failed. Name the
			// part and sha, and say what the condition actually means:
			// ErrNoLinkTargetAvail describes the failed public-object
			// fallback, not the real problem, which is that this owner has no
			// usable copy of the object.
			switch {
			case errors.Is(err, objects.ErrNoLinkTargetAvail):
				return nil, fmt.Errorf(
					"state_object: no stored object for part %q (sha %s) under owner %s, and no public object available to link to: %w",
					key, sha, owner, err)
			case errors.Is(err, objects.ErrNoBackingFile):
				return nil, fmt.Errorf(
					"state_object: part %q (sha %s) is registered for owner %s but its content is missing from storage: %w",
					key, sha, owner, err)
			case errors.Is(err, mongo.ErrNoDocuments):
				return nil, fmt.Errorf(
					"state_object: no object registered for part %q (sha %s) under owner %s: %w",
					key, sha, owner, err)
			default:
				return nil, fmt.Errorf(
					"state_object: could not resolve part %q (sha %s) for owner %s: %w",
					key, sha, owner, err)
			}
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
	if err != nil {
		return errors.New("unmark_object_as_garbage:Error updating object:" + err.Error())
	}
	if updateResult.MatchedCount == 0 {
		return errors.New("unmark_object_as_garbage:Error updating object: not found")
	}
	return nil
}

// IsDevicePublic checks if a device is public or not
func (a *App) IsDevicePublic(ctx context.Context, ID primitive.ObjectID) (bool, error) {

	devicesApp := devices.Build(a.mongoClient, nil)
	device := devices.Device{}

	err := devicesApp.FindDeviceByID(ctx, ID, &device)
	if err != nil {
		return false, err
	}
	return device.IsPublic, nil
}
