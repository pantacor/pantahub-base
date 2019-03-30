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
	"strconv"
	time "time"

	"github.com/mongodb/mongo-go-driver/bson/primitive"
	"github.com/mongodb/mongo-go-driver/mongo/options"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gopkg.in/mgo.v2/bson"
)

// step wanted can be added by the device owner or delegate.
// steps that were not reported can be deleted still. other steps
// cannot be deleted until the device gets deleted as well.
// Step: Step model
type Step struct {
	ID               string                 `json:"id" bson:"_id"` // XXX: make type
	Owner            string                 `json:"owner"`
	Device           string                 `json:"device"`
	Committer        string                 `json:"committer"`
	TrailID          bson.ObjectId          `json:"trail-id" bson:"trail-id"` //parent id
	Rev              int                    `json:"rev"`
	CommitMsg        string                 `json:"commit-msg" bson:"commit-msg"`
	State            map[string]interface{} `json:"state"` // json blurb
	StateSha         string                 `json:"state-sha" bson:"statesha"`
	StepProgress     StepProgress           `json:"progress" bson:"progress"`
	StepTime         time.Time              `json:"step-time" bson:"step-time"`
	ProgressTime     time.Time              `json:"progress-time" bson:"progress-time"`
	Garbage          bool                   `json:"garbage" bson:"garbage"`
	GarbageRemovalAt time.Time              `bson:"garbage_removal_at" json:"garbage_removal_at"`
	GcProcessed      bool                   `json:"gc_processed" bson:"gc_processed"`
	UsedObjects      []string               `bson:"used_objects" json:"used_objects"`
}

// StepProgress : StepProgress model
type StepProgress struct {
	Progress  int    `json:"progress"`                    // progress number. steps or 1-100
	StatusMsg string `json:"status-msg" bson:"statusmsg"` // message of progress status
	Status    string `json:"status"`                      // status code
	Log       string `json:"log"`                         // log if available
}

// markStepsAsGarbage : Mark Steps Aa Garbage
func markStepsAsGarbage(deviceID bson.ObjectId) (
	result bool,
	stepsMarked int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	c := db.C("pantahub_steps")
	GarbageRemovalAt, parseErrors := GetGarbageRemovalTime()
	if len(parseErrors) > 0 {
		errs = MergeErrors(errs, parseErrors)
		return false, 0, errs
	}
	info, err := c.UpdateAll(bson.M{"trail-id": deviceID},
		bson.M{"$set": bson.M{
			"garbage":            true,
			"garbage_removal_at": GarbageRemovalAt,
			"gc_processed":       false}})
	if utils.GetEnv("DEBUG") == "true" {
		log.Printf("%+v\n", info)
	}
	if err != nil {
		errs.Add("mark_all_trail_steps_as_garbage", err.Error()+"[trail-id:"+deviceID.Hex()+"]")
		return false, info.Updated, errs
	}
	return true, info.Updated, errs
}

// ProcessStepGarbages : Process Step Garbages
func (step *Step) ProcessStepGarbages() (
	result bool,
	stepsProcessed int,
	objectsMarkedAsGarbage int,
	stepsWithErrors int,
	objectsWithErrorsCount int,
	objectsIgnoredTotalCount int,
	warnings url.Values,
	errs url.Values,
) {
	errs = url.Values{}
	warnings = url.Values{}
	collection := db.MongoDb.Collection("pantahub_steps")
	ctx := context.Background()
	findOptions := options.Find()
	findOptions.SetHint(bson.M{"_id": 1}) //Index fields
	findOptions.SetNoCursorTimeout(true)
	// Fetch all steps documents with (garbage:true AND (gc_processed:false if exist OR gc_processed not exist ))
	/* Note: Actual Record fetching will not happen here
	as it is using mongodb cursor and record fetching will
	start with we call cur.Next()*/
	// Create mongo cursor
	cur, err := collection.Find(ctx, bson.M{
		"garbage":      true,
		"gc_processed": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		errs.Add("find_steps", err.Error())
	}
	defer cur.Close(ctx)
	stepsProcessed = 0
	objectsMarkedAsGarbage = 0
	objectsMarkedAsGarbageList := []string{}
	stepsWithErrors = 0
	objectsWithErrorsCount = 0
	objectsIgnoredTotalCount = 0
	objectsIgnored := []string{}
	i := 0
	for cur.Next(ctx) {
		result := bson.M{}
		err := cur.Decode(&result)
		if err != nil {
			errs.Add("cursor_decode_error", err.Error())
		}
		step := Step{}
		step.ID = result["_id"].(string)
		step.State = result["state"].(primitive.D).Map()
		step.Owner = result["owner"].(string)
		if utils.GetEnv("DEBUG") == "true" {
			i++
			log.Print("Processing Step:" + strconv.Itoa(i))
		}
		_, _, _, _, stepErrors := step.populateStepsUsedObjects()
		if len(stepErrors) > 0 {
			errs = MergeErrors(errs, stepErrors)
			stepsWithErrors++
		}
		_,
			objectsMarkedList,
			objectsWithErrors,
			newObjectsIgnoredList,
			_,
			objectErrors := markObjectsAsGarbage(step.UsedObjects)

		/*populating objects Marked  list,Note: Purpose of
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
		objectsWithErrorsCount += objectsWithErrors
		if len(objectErrors) > 0 {
			errs = MergeErrors(errs, objectErrors)
		} else {
			//Marking step as gc_processed=true
			err := db.Session.C("pantahub_steps").Update(
				bson.M{"_id": step.ID},
				bson.M{"$set": bson.M{"gc_processed": true}})
			if err != nil {
				errs.Add("mark_step_as_gc_processed", err.Error()+"[Step ID:"+step.ID+"]")
			} else {
				stepsProcessed++
			}
		}

	}
	//populate Marked Objects Count
	objectsMarkedAsGarbage += len(objectsMarkedAsGarbageList)
	//Populating warning messages for ignored objects
	if len(objectsIgnored) > 0 {
		for _, v := range objectsIgnored {
			warnings.Add("objects_ignored", "Ignorinng step Object storage-id(_id):"+v+" due to more than 0 usage")
		}
	}
	objectsIgnoredTotalCount = len(objectsIgnored)

	if len(errs) > 0 {
		return false,
			stepsProcessed,
			objectsMarkedAsGarbage,
			stepsWithErrors,
			objectsWithErrorsCount,
			objectsIgnoredTotalCount,
			warnings,
			errs
	}
	return true,
		stepsProcessed,
		objectsMarkedAsGarbage,
		stepsWithErrors,
		objectsWithErrorsCount,
		objectsIgnoredTotalCount,
		warnings,
		errs
}

// populateUsedObjects : Populate used_objects field in trail
func (step *Step) populateStepsUsedObjects() (
	result bool,
	ObjectList []string,
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
		stateErrors := parseStateObjects(step.Owner, step.State, "Step ID:"+step.ID)
	if len(stateErrors) > 0 {
		errs = MergeErrors(errs, stateErrors)
	}
	step.UsedObjects = objectList
	if InvalidObjectCount == 0 && !InvalidState {
		err := db.C("pantahub_steps").Update(
			bson.M{"_id": step.ID},
			bson.M{"$set": bson.M{"used_objects": objectList}})
		if err != nil {
			errs.Add("update_steps_used_objects", err.Error()+"[Step ID:"+step.ID+"]")
			return false, objectList, InvalidObjectCount, InvalidState, errs
		}
		if utils.GetEnv("DEBUG") == "true" {
			log.Print("\nPopulated used_objects List for step id:" + step.ID)
		}
		return true, objectList, InvalidObjectCount, InvalidState, errs
	}
	return false, objectList, InvalidObjectCount, InvalidState, errs
}

// deleteStepsGarbage : Delete Steps  Garbage
func deleteStepsGarbage() (
	result bool,
	StepsRemoved int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	c := db.C("pantahub_steps")
	now := time.Now().Local()
	//"garbage_removal_at": bson.M{"$lt": now}
	info, err := c.RemoveAll(bson.M{
		"garbage":            true,
		"garbage_removal_at": bson.M{"$lt": now},
	})
	if err != nil {
		errs.Add("delete_all_steps", err.Error())
		return false, 0, errs
	}
	return true, info.Removed, errs
}

// PopulateAllStepsUsedObjects : Populate used_objects_field for all steps
func (step *Step) PopulateAllStepsUsedObjects() (
	result bool,
	stepsPopulated int,
	stepsWithErrors int,
	errs url.Values,
) {
	errs = url.Values{}
	collection := db.MongoDb.Collection("pantahub_steps")
	ctx := context.Background()
	findOptions := options.Find()
	findOptions.SetHint(bson.M{"_id": 1}) //Index fields
	findOptions.SetNoCursorTimeout(true)
	// Fetch all steps documents without used_objects field
	/* Note: Actual Record fetching will not happen here
	as it is using mongodb cursor and record fetching will
	start with we call cur.Next() */
	cur, err := collection.Find(ctx, bson.M{
		"used_objects": bson.M{"$exists": false}}, findOptions)
	if err != nil {
		errs.Add("find_steps", err.Error())
	}
	defer cur.Close(ctx)
	stepsPopulated = 0
	stepsWithErrors = 0
	for cur.Next(ctx) {
		result := bson.M{}
		err := cur.Decode(&result)
		if err != nil {
			errs.Add("cursor_decode_error", err.Error())
		}
		step := Step{}
		step.ID = result["_id"].(string)
		step.State = result["state"].(primitive.D).Map()
		step.Owner = result["owner"].(string)
		_, _,
			InvalidObjectCount,
			InvalidState,
			stepErrors := step.populateStepsUsedObjects()
		if len(stepErrors) > 0 {
			errs = MergeErrors(errs, stepErrors)
			stepsWithErrors++
		}
		if InvalidObjectCount == 0 && !InvalidState {
			stepsPopulated++
		}
	}
	if len(errs) > 0 {
		return false, stepsPopulated, stepsWithErrors, errs
	}
	return true, stepsPopulated, stepsWithErrors, errs
}
