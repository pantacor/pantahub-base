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
	"log"
	"net/url"
	time "time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gopkg.in/mgo.v2/bson"
)

// Object : Object model
type Object struct {
	ID               string `json:"id" bson:"id"`
	StorageID        string `json:"storage-id" bson:"_id"`
	Owner            string `json:"owner"`
	ObjectName       string `json:"objectname"`
	Sha              string `json:"sha256sum"`
	Size             string `json:"size"`
	SizeInt          int64  `json:"sizeint"`
	MimeType         string `json:"mime-type"`
	initialized      bool
	Garbage          bool      `json:"garbage" bson:"garbage"`
	GarbageRemovalAt time.Time `bson:"garbage_removal_at" json:"garbage_removal_at"`
}

// markObjectsAsGarbage : Mark Objects as Garbage
func markObjectsAsGarbage(objects []string) (
	result bool,
	objectsMarkedAsGarbageList []string,
	objectsWithErrors int,
	objectsIgnored []string,
	objectsIgnoredCount int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	GarbageRemovalAt, parseErrors := GetGarbageRemovalTime()
	if len(parseErrors) > 0 {
		errs = MergeErrors(errs, parseErrors)
		return false, nil, 0, nil, 0, errs
	}
	objectsMarkedAsGarbage := 0
	objectsWithErrors = 0
	objectsIgnoredCount = 0
	objectsMarkedAsGarbageList = []string{}

	for _, object := range objects {
		result, _ := IsObjectValid(object)
		if !result {
			objectsWithErrors++
		}
		totalUsage, _, _,
			usageErrors := getObjectUsageInfo(object)
		if len(usageErrors) > 0 {
			errs = MergeErrors(errs, usageErrors)
		}
		log.Print("TTOTAL OBJECT USAGE:")
		log.Print(totalUsage)
		if totalUsage > 0 {
			log.Print("Ignoring object:" + object)
			objectsIgnored = append(objectsIgnored, object)
			objectsIgnoredCount++
		} else {
			if utils.GetEnv("DEBUG") == "true" {
				log.Print("\nObject:" + object)
			}
			err := db.C("pantahub_objects").Update(
				bson.M{"_id": object},
				bson.M{"$set": bson.M{
					"garbage":            true,
					"garbage_removal_at": GarbageRemovalAt}})
			if err != nil {
				errs.Add("mark_object_as_garbage", err.Error()+"[Object ID:"+object+"]")
			} else {

				if !contains(objectsMarkedAsGarbageList, object) {
					objectsMarkedAsGarbage++
					objectsMarkedAsGarbageList = append(objectsMarkedAsGarbageList, object)
				}

			}
		}
	}
	if len(errs) > 0 {
		return false,
			objectsMarkedAsGarbageList,
			objectsWithErrors,
			objectsIgnored,
			objectsIgnoredCount,
			errs
	}
	return true,
		objectsMarkedAsGarbageList,
		objectsWithErrors,
		objectsIgnored,
		objectsIgnoredCount,
		errs
}

// deleteAllObjectsGarbage : Delete Objects  Garbage
func deleteAllObjectsGarbage() (
	result bool,
	ObjectsRemoved int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	c := db.C("pantahub_objects")
	now := time.Now().Local()
	info, err := c.RemoveAll(
		bson.M{
			"garbage":            true,
			"garbage_removal_at": bson.M{"$lt": now},
		})
	if err != nil {
		errs.Add("remove_all_objects", err.Error())
		return false, 0, errs
	}
	if utils.GetEnv("DEBUG") == "true" {
		log.Print("Objects removed:")
		log.Print(info.Removed)
	}
	return true, info.Removed, errs

}

// getObjectUsageInfo : Get Object Usage Information
func getObjectUsageInfo(objectString string) (
	totalUsage int,
	usageInTrails int,
	usageInSteps int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	usageInTrails, err := db.C("pantahub_trails").Find(bson.M{
		"used_objects": objectString,
		"garbage":      bson.M{"$ne": true},
	}).Count()
	if err != nil {
		errs.Add("find_object_used_count_in_trails", err.Error())
	}
	usageInSteps, err = db.C("pantahub_steps").Find(bson.M{
		"used_objects": objectString,
		"garbage":      bson.M{"$ne": true},
	}).Count()
	if err != nil {
		errs.Add("find_object_used_count_in_steps", err.Error())
	}
	totalUsage = (usageInTrails + usageInSteps)
	return totalUsage, usageInTrails, usageInSteps, errs
}

// IsObjectValid : to check if an object is valid or not
func IsObjectValid(ObjectID string) (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	count, err := db.C("pantahub_objects").Find(bson.M{"_id": ObjectID}).Count()
	if err != nil {
		errs.Add("object", err.Error())
	}
	return (count == 1), errs
}

// IsObjectGarbage : to check if an object is garbage or not
func IsObjectGarbage(ObjectID string) (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	count, err := db.C("pantahub_objects").Find(bson.M{"_id": ObjectID, "garbage": true}).Count()
	if err != nil {
		errs.Add("object", err.Error())
	}
	return (count == 1), errs
}

// UnMarkObjectAsGarbage : to unmark object as garbage
func UnMarkObjectAsGarbage(ObjectID string) (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	err := db.C("pantahub_objects").Update(
		bson.M{"_id": ObjectID},
		bson.M{"$set": bson.M{
			"garbage": false,
		}})
	if err != nil {
		errs.Add("unmark_object_as_garbage", err.Error()+"[ID:"+ObjectID+"]")
		return false, errs
	}
	return true, errs
}
