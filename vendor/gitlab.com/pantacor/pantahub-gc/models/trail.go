//
// Copyright 2018  Pantacor Ltd.
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

package models

import (
	"context"
	"log"
	"net/url"
	"strings"
	time "time"

	"github.com/mongodb/mongo-go-driver/bson/primitive"
	"github.com/mongodb/mongo-go-driver/mongo/options"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gopkg.in/mgo.v2/bson"
)

// Trail : Trail model
type Trail struct {
	ID     bson.ObjectId `json:"id" bson:"_id"`
	Owner  string        `json:"owner"`
	Device string        `json:"device"`
	//  Admins   []string `json:"admins"`   // XXX: maybe this is best way to do delegating device access....
	LastInSync       time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched      time.Time              `json:"last-touched" bson:"last-touched"`
	FactoryState     map[string]interface{} `json:"factory-state" bson:"factory-state"`
	Garbage          bool                   `json:"garbage" bson:"garbage"`
	GarbageRemovalAt time.Time              `bson:"garbage_removal_at" json:"garbage_removal_at"`
	GcProcessed      bool                   `json:"gc_processed" bson:"gc_processed"`
	UsedObjects      []string               `bson:"used_objects" json:"used_objects"`
}

// MarkAllTrailGarbages : Mark all trail garbages
func MarkAllTrailGarbages() (
	result bool,
	trailsMarked int,
	errs url.Values,
) {
	errs = url.Values{}
	collection := db.MongoDb.Collection("pantahub_trails")
	ctx := context.Background()
	trailsMarked = 0
	trailsWithErrors := []string{}
	findOptions := options.Find()
	findOptions.SetHint(bson.M{"_id": 1}) //Index fields
	findOptions.SetNoCursorTimeout(true)
	/* Note: Actual Record fetching will not happen here
	as it is using mongodb cursor and record fetching will
	start with we call cur.Next() */
	// Create mongo cursor
	cur, err := collection.Find(ctx, bson.M{
		"garbage": bson.M{"$ne": true}}, findOptions)
	if err != nil {
		errs.Add("find_trails", err.Error())
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := map[string]interface{}{}
		err := cur.Decode(&result)
		if err != nil {
			errs.Add("cursor_decode_error", err.Error())
		}
		trail := Trail{}
		trailID := result["_id"].(primitive.ObjectID).Hex()
		trail.ID = bson.ObjectIdHex(trailID)
		if contains(trailsWithErrors, trailID) {
			continue
		}
		deviceResult, deviceErrors := IsDeviceValid(trail.ID)
		if len(deviceErrors) > 0 {
			errs = MergeErrors(errs, deviceErrors)
			trailsWithErrors = append(trailsWithErrors, trail.ID.Hex())
		}
		if !deviceResult {
			trailResult, trailErrors := markTrailAsGarbage(trail.ID)
			if len(trailErrors) > 0 {
				errs = MergeErrors(errs, trailErrors)
			}
			if trailResult {
				trailsMarked++
			}
		}
	}
	if err := cur.Err(); err != nil {
		errs.Add("cursor_error", err.Error())
	}
	if len(errs) > 0 {
		return false, trailsMarked, errs
	}
	return true, trailsMarked, errs
}

// markTrailAsGarbage : Mark Trail A sGarbage
func markTrailAsGarbage(deviceID bson.ObjectId) (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	GarbageRemovalAt, parseErrors := GetGarbageRemovalTime()
	if len(parseErrors) > 0 {
		errs = MergeErrors(errs, parseErrors)
		return false, errs
	}
	err := db.C("pantahub_trails").Update(
		bson.M{"_id": deviceID},
		bson.M{"$set": bson.M{
			"garbage":            true,
			"garbage_removal_at": GarbageRemovalAt,
			"gc_processed":       false}})
	if err != nil {
		errs.Add("mark_trail_as_garbage", err.Error()+"[Trail ID:"+deviceID.Hex()+"]")
		return false, errs
	}
	trail := Trail{}
	err = db.C("pantahub_trails").Find(bson.M{"_id": deviceID}).One(&trail)
	if err != nil {
		errs.Add("trail", err.Error())
	}
	// keep used_objects of trails as upto date
	trail.populateTrailUsedObjects()
	// keep used_objects of step(rev=0) as upto date
	step := Step{}
	step.ID = trail.ID.Hex() + "-0"
	step.Owner = trail.Owner
	step.State = trail.FactoryState
	step.populateStepsUsedObjects()

	return true, errs
}

// ProcessTrailGarbages : Process Trail Garbages
func (trail *Trail) ProcessTrailGarbages() (
	result bool,
	trailsProcessed int,
	stepsMarkedAsGarbage int,
	objectsMarkedAsGarbage int,
	trailsWithErrors int,
	objectsWithErrorsCount int,
	objectsIgnoredTotalCount int,
	stepsWithErrors int,
	warnings url.Values,
	errs url.Values,
) {
	errs = url.Values{}
	warnings = url.Values{}
	trailsProcessed = 0
	stepsMarkedAsGarbage = 0
	objectsMarkedAsGarbage = 0
	objectsMarkedAsGarbageList := []string{}
	trailsWithErrors = 0
	objectsWithErrorsCount = 0
	objectsIgnoredTotalCount = 0
	stepsWithErrors = 0
	objectsIgnored := []string{}
	collection := db.MongoDb.Collection("pantahub_trails")
	ctx := context.Background()
	findOptions := options.Find()
	findOptions.SetHint(bson.M{"_id": 1}) //Index fields
	findOptions.SetNoCursorTimeout(true)
	// Fetch all trail documents with (garbage:true AND (gc_processed:false if exist OR gc_processed not exist ))
	/* Note: Actual Record fetching will not happen here
	as it is using mongodb cursor and record fetching will
	start with we call cur.Next() */
	// Create mongo cursor
	cur, err := collection.Find(ctx, bson.M{
		"garbage":      true,
		"gc_processed": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		errs.Add("find_trails", err.Error())
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := bson.M{}
		err := cur.Decode(&result)
		if err != nil {
			errs.Add("cursor_decode_error", err.Error())
		}
		//fmt.Println(reflect.TypeOf(result))
		//fmt.Println(reflect.TypeOf(result["factory-state"]))
		trail := Trail{}
		trailID := result["_id"].(primitive.ObjectID).Hex()
		trail.ID = bson.ObjectIdHex(trailID)
		trail.FactoryState = result["factory-state"].(primitive.D).Map()
		trail.Owner = result["owner"].(string)
		_,
			_, InvalidObjectCount,
			_,
			trailErrors := trail.populateTrailUsedObjects()
		if len(trailErrors) > 0 {
			errs = MergeErrors(errs, trailErrors)
			trailsWithErrors++
			if InvalidObjectCount > 0 {
				objectsWithErrorsCount += InvalidObjectCount
				stepsWithErrors++ //for steps with rev=0
			}
		} else {
			//1.Mark Steps As Garbage
			_, stepsMarked, stepErrors := markStepsAsGarbage(trail.ID)
			stepsMarkedAsGarbage += stepsMarked
			if len(stepErrors) > 0 {
				errs = MergeErrors(errs, stepErrors)
				stepsWithErrors++
			}
			//2.Mark Objects As Garbage
			_,
				objectsMarkedList,
				objectsWithErrors,
				newObjectsIgnoredList,
				_,
				objectErrors := markObjectsAsGarbage(trail.UsedObjects)
			objectsWithErrorsCount += objectsWithErrors

			if len(objectErrors) > 0 {
				errs = MergeErrors(errs, objectErrors)
			}
			/* Populating objects Marked  list,Note: Purpose of
			these 2 below for loops are to avoid counts of duplicate marked/ignored
			objects count when there is same object is used in multiple trails/steps */
			for _, v := range objectsMarkedList {
				if !contains(objectsMarkedAsGarbageList, v) {
					objectsMarkedAsGarbageList = append(objectsMarkedAsGarbageList, v)
				}
			}
			//populating igonored object list
			for _, v := range newObjectsIgnoredList {
				if !contains(objectsIgnored, v) {
					objectsIgnored = append(objectsIgnored, v)
				}
			}

		}
		//Marking trail as gc_processed=true
		if len(trailErrors) == 0 {
			err = db.Session.C("pantahub_trails").Update(
				bson.M{"_id": trail.ID},
				bson.M{"$set": bson.M{"gc_processed": true}})
			if err != nil {
				errs.Add("marking_trail_as_gc_processed", err.Error()+"[ID:"+trail.ID.Hex()+"]")
			} else {
				trailsProcessed++
			}
		}

	}
	//populate Marked Objects Count
	objectsMarkedAsGarbage += len(objectsMarkedAsGarbageList)
	//Populating warning messages for ignored objects
	if len(objectsIgnored) > 0 {
		for _, v := range objectsIgnored {
			warnings.Add("objects_ignored", "Ignorinng trail Object storage-id(_id):"+v+" due to more than 0 usage")
		}
	}
	//populate Ignored Objects Count
	objectsIgnoredTotalCount = len(objectsIgnored)
	if len(errs) > 0 {
		return false,
			trailsProcessed,
			stepsMarkedAsGarbage,
			objectsMarkedAsGarbage,
			trailsWithErrors,
			objectsWithErrorsCount,
			objectsIgnoredTotalCount,
			stepsWithErrors,
			warnings,
			errs
	}
	return true,
		trailsProcessed,
		stepsMarkedAsGarbage,
		objectsMarkedAsGarbage,
		trailsWithErrors,
		objectsWithErrorsCount,
		objectsIgnoredTotalCount,
		stepsWithErrors,
		warnings,
		errs
}

// populateTrailUsedObjects : Populate used_objects field in trail collection
func (trail *Trail) populateTrailUsedObjects() (
	result bool,
	objectList []string,
	InvalidObjectCount int,
	InvalidState bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	_,
		objectList,
		InvalidObjectCount,
		InvalidState,
		stateErrors := parseStateObjects(trail.Owner, trail.FactoryState, "Trail ID:"+trail.ID.Hex())
	if len(stateErrors) > 0 {
		errs = MergeErrors(errs, stateErrors)
	}
	if utils.GetEnv("DEBUG") == "true" {
		log.Print("\nPopulated trail used_objects List")
	}
	trail.UsedObjects = objectList
	if InvalidObjectCount == 0 && !InvalidState {
		err := db.C("pantahub_trails").Update(
			bson.M{"_id": trail.ID},
			bson.M{"$set": bson.M{"used_objects": objectList}})
		if err != nil {
			errs.Add("update_trail_used_object", err.Error()+"[ID:"+trail.ID.Hex()+"]")
			return false, objectList, InvalidObjectCount, InvalidState, errs
		}
		return true, objectList, InvalidObjectCount, InvalidState, errs
	}
	return false, objectList, InvalidObjectCount, InvalidState, errs
}

// parseStateObjects: Parse State Objects and populate ObjectList
func parseStateObjects(owner string, state map[string]interface{}, model string) (
	result bool,
	objs []string,
	InvalidObjectsCount int,
	InvalidState bool,
	errs url.Values,
) {
	errs = url.Values{}
	objMap := map[string]bool{}
	objs = []string{}
	InvalidObjectsCount = 0
	InvalidState = true
	for key, v := range state {
		if key == "#spec" && v == "pantavisor-multi-platform@1" {
			InvalidState = false
			break
		}
	}
	if InvalidState {
		errs.Add("state_object", "Invalid state:#spec is missing or value is invalid ["+model+"]")
		return false, objs, InvalidObjectsCount, InvalidState, errs
	}
	for key, v := range state {
		if strings.HasSuffix(key, ".json") ||
			strings.HasSuffix(key, "Ｎjson") ||
			key == "#spec" {
			continue
		}
		sha, found := v.(string)
		if !found {
			errs.Add("state_object", "Object is not a string[sha:"+sha+","+model+"]")
			InvalidObjectsCount++
			continue
		}
		shaBytes, err := utils.DecodeSha256HexString(sha)
		if err != nil {
			errs.Add("state_object", "Object sha that could not be decoded from hex:"+err.Error()+" [sha:"+sha+","+model+"]")
			InvalidObjectsCount++
			continue
		}
		// lets use proper storage shas to reflect that fact that each
		// owner has its own copy of the object instance on DB side
		storageSha := objects.MakeStorageId(owner, shaBytes)
		result, _ := IsObjectValid(storageSha)
		if !result {
			errs.Add("state_object", "Object sha is not found in the db[storage-id(_id):"+storageSha+","+model+"]")
			InvalidObjectsCount++
			continue
		}
		if _, ok := objMap[storageSha]; !ok {
			result, objectErrors := IsObjectGarbage(storageSha)
			if len(objectErrors) > 0 {
				errs = MergeErrors(errs, objectErrors)
			}
			if result {
				_, objectErrors := UnMarkObjectAsGarbage(storageSha)
				if len(objectErrors) > 0 {
					errs = MergeErrors(errs, objectErrors)
				}
			}
			objMap[storageSha] = true
			objs = append(objs, storageSha)
		}
	}
	if InvalidObjectsCount > 0 {
		return false, objs, InvalidObjectsCount, InvalidState, errs
	}
	return true, objs, InvalidObjectsCount, InvalidState, errs
}

// deleteTrailGarbage : Delete Trail Garbage
func deleteTrailGarbage() (
	result bool,
	trailsRemoved int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	c := db.C("pantahub_trails")
	now := time.Now().Local()
	info, err := c.RemoveAll(
		bson.M{"garbage": true,
			"garbage_removal_at": bson.M{"$lt": now},
		})
	if err != nil {
		errs.Add("remove_all_trails", err.Error())
		return false, 0, errs
	}
	return true, info.Removed, errs
}

// PopulateAllTrailsUsedObjects : Populate used_objects_field for all trails
func (trail *Trail) PopulateAllTrailsUsedObjects() (
	result bool,
	trailsPopulated int,
	trailsWithErrors int,
	errs url.Values,
) {
	errs = url.Values{}
	collection := db.MongoDb.Collection("pantahub_trails")
	ctx := context.Background()
	findOptions := options.Find()
	findOptions.SetHint(bson.M{"_id": 1}) //Index fields
	findOptions.SetNoCursorTimeout(true)
	// Fetch all trail documents without used_objects field
	/* Note: Actual Record fetching will not happen here
	as it is using mongodb cursor and record fetching will
	start with we call cur.Next() */
	// Create mongo cursor
	cur, err := collection.Find(ctx, bson.M{
		"used_objects": bson.M{"$exists": false},
	}, findOptions)
	if err != nil {
		errs.Add("find_trails", err.Error())
	}
	defer cur.Close(ctx)
	trailsPopulated = 0
	trailsWithErrors = 0
	for cur.Next(ctx) {
		result := bson.M{}
		err := cur.Decode(&result)
		if err != nil {
			errs.Add("cursor_decode_error", err.Error())
		}
		trail := Trail{}
		trailID := result["_id"].(primitive.ObjectID).Hex()
		trail.ID = bson.ObjectIdHex(trailID)
		trail.FactoryState = result["factory-state"].(primitive.D).Map()
		trail.Owner = result["owner"].(string)
		_, _,
			InvalidObjectCount,
			InvalidState,
			trailErrors := trail.populateTrailUsedObjects()
		if len(trailErrors) > 0 {
			errs = MergeErrors(errs, trailErrors)
		}
		if InvalidObjectCount == 0 && !InvalidState {
			trailsPopulated++
		} else {
			trailsWithErrors++
		}
	}
	if len(errs) > 0 {
		return false,
			trailsPopulated,
			trailsWithErrors,
			errs
	}
	return true,
		trailsPopulated,
		trailsWithErrors,
		errs
}

// contains : to check if array contains an item or not
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
