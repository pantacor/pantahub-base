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
	"net/http"
	"net/url"
	"strconv"
	time "time"

	duration "github.com/ChannelMeter/iso8601duration"
	"github.com/gorilla/mux"
	"github.com/mongodb/mongo-go-driver/bson/primitive"
	"github.com/mongodb/mongo-go-driver/mongo/options"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gopkg.in/mgo.v2/bson"
)

// Device : Device model
type Device struct {
	ID               bson.ObjectId          `json:"id" bson:"_id"`
	Prn              string                 `json:"prn"`
	Nick             string                 `json:"nick"`
	Owner            string                 `json:"owner"`
	OwnerNick        string                 `json:"owner-nick" bson:"-"`
	Secret           string                 `json:"secret,omitempty"`
	TimeCreated      time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified     time.Time              `json:"time-modified" bson:"timemodified"`
	Challenge        string                 `json:"challenge,omitempty"`
	IsPublic         bool                   `json:"public"`
	UserMeta         map[string]interface{} `json:"user-meta" bson:"user-meta"`
	DeviceMeta       map[string]interface{} `json:"device-meta" bson:"device-meta"`
	Garbage          bool                   `json:"garbage" bson:"garbage"`
	GarbageRemovalAt time.Time              `bson:"garbage_removal_at" json:"garbage_removal_at"`
	GcProcessed      bool                   `json:"gc_processed" bson:"gc_processed"`
}

// MarkDeviceAsGrabage : Mark a device as garbage
func (device *Device) MarkDeviceAsGrabage() (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	c := db.C("pantahub_devices")
	GarbageRemovalAt, parseErrors := GetGarbageRemovalTime()
	if len(parseErrors) > 0 {
		errs = MergeErrors(errs, parseErrors)
		return false, errs
	}
	err := c.Update(bson.M{"_id": device.ID},
		bson.M{"$set": bson.M{
			"garbage":            true,
			"garbage_removal_at": GarbageRemovalAt}})
	if err != nil {
		errs.Add("mark_device_as_garbage", err.Error()+"[ID:"+device.ID.Hex()+"]")
		return false, errs
	}
	device.Garbage = true
	device.GarbageRemovalAt = GarbageRemovalAt
	return true, errs
}

// MarkUnClaimedDevicesAsGrabage :  Mark all unclaimed devices as garbage after a while(eg: after 5 days)
func (device *Device) MarkUnClaimedDevicesAsGrabage() (
	result bool,
	devicesMarked int,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	c := db.C("pantahub_devices")
	GarbageRemovalAt, parseErrors := GetGarbageRemovalTime()
	if len(parseErrors) > 0 {
		errs = MergeErrors(errs, parseErrors)
		return false, 0, errs
	}
	TimeLeftForGarbaging := utils.GetEnv("PANTAHUB_GC_UNCLAIMED_EXPIRY")
	parsedDuration, err := duration.FromString(TimeLeftForGarbaging)
	if err != nil {
		errs.Add("parsing_iso8601", err.Error()+"[ID:"+device.ID.Hex()+"]")
		return false, 0, errs
	}
	TimeBeforeDuration := time.Now().Local().Add(-parsedDuration.ToDuration())
	if utils.GetEnv("DEBUG") == "true" {
		log.Print("TimeBeforeDuration:")
		log.Print(TimeBeforeDuration)
		log.Print("GarbageRemovalAt:")
		log.Print(GarbageRemovalAt)
	}
	info, err := c.UpdateAll(
		bson.M{"challenge": bson.M{"$ne": ""},
			"timecreated": bson.M{"$lt": TimeBeforeDuration},
			"garbage":     bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{
			"garbage":            true,
			"garbage_removal_at": GarbageRemovalAt}})

	if err != nil {
		errs.Add("mark_unclaimed_device_as_garbage", err.Error()+"[ID:"+device.ID.Hex()+"]")
		return false, info.Updated, errs
	}
	return true, info.Updated, errs
}

// ProcessDeviceGarbages : Process Device Garbages
func (device *Device) ProcessDeviceGarbages() (
	result bool,
	deviceProcessed int,
	trailsMarkedAsGarbage int,
	trailsWithErrors int,
	errs url.Values,
) {
	errs = url.Values{}

	collection := db.MongoDb.Collection("pantahub_devices")
	ctx := context.Background()
	findOptions := options.Find()
	findOptions.SetHint(bson.M{"_id": 1}) //Index fields
	findOptions.SetNoCursorTimeout(true)
	//Fetch all device documents with (garbage:true AND (gc_processed:false if exist OR gc_processed not exist ))
	/* Note: Actual Record fetching will not happen here
	as it is using mongodb cursor and record fetching will
	start with we call cur.Next()
	*/
	cur, err := collection.Find(ctx, bson.M{
		"garbage":      true,
		"gc_processed": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		errs.Add("find_devices", err.Error())
	}
	defer cur.Close(ctx)
	deviceProcessed = 0
	trailsMarkedAsGarbage = 0
	trailsWithErrors = 0
	i := 0
	for cur.Next(ctx) {
		result := bson.M{}
		err := cur.Decode(&result)
		if err != nil {
			errs.Add("cursor_decode_error", err.Error())
		}
		if utils.GetEnv("DEBUG") == "true" {
			i++
			log.Print("Processing Device:" + strconv.Itoa(i))
		}

		device := Device{}
		deviceID := result["_id"].(primitive.ObjectID).Hex()
		device.ID = bson.ObjectIdHex(deviceID)
		trailResult, trailErrs := markTrailAsGarbage(device.ID)
		if len(trailErrs) > 0 {
			errs = MergeErrors(errs, trailErrs)
			trailsWithErrors++
		}
		if trailResult {
			trailsMarkedAsGarbage++
			//Marking device as gc_processed=true
			err := db.Session.C("pantahub_devices").Update(
				bson.M{"_id": device.ID},
				bson.M{"$set": bson.M{"gc_processed": true}})
			if err != nil {
				errs.Add("mark_device_as_gc_processed", err.Error()+"[ID:"+device.ID.Hex()+"]")
			} else {
				deviceProcessed++
			}
		}
	}
	if len(errs) > 0 {
		return false,
			deviceProcessed,
			trailsMarkedAsGarbage,
			trailsWithErrors,
			errs
	}
	return true,
		deviceProcessed,
		trailsMarkedAsGarbage,
		trailsWithErrors,
		errs
}

// DeleteGarbages : Delete Garbages of a device
func (device *Device) DeleteGarbages() (
	result bool,
	response map[string]interface{},
) {
	result1 := false
	result2 := false
	result3 := false
	trailsRemoved := 0
	stepsRemoved := 0
	objectsRemoved := 0
	trailErrors := url.Values{}
	stepErrors := url.Values{}
	objectErrors := url.Values{}

	if utils.GetEnv("PANTAHUB_GC_REMOVE_GARBAGE") == "true" {
		// 1.Delete Trails
		result1, trailsRemoved, trailErrors = deleteTrailGarbage()

		// 2.Delete Steps
		result2, stepsRemoved, stepErrors = deleteStepsGarbage()

		// 3.Delete Objects
		result3, objectsRemoved, objectErrors = deleteAllObjectsGarbage()
	}

	if !result1 || !result2 || !result3 {

		return false, map[string]interface{}{
			"status": 0,
			"trails": map[string]interface{}{
				"status":         0,
				"trails_removed": trailsRemoved,
				"errors":         trailErrors,
			},
			"steps": map[string]interface{}{
				"status":        0,
				"steps_removed": stepsRemoved,
				"errors":        stepErrors,
			},
			"objects": map[string]interface{}{
				"status":          0,
				"objects_removed": objectsRemoved,
				"errors":          objectErrors,
			},
		}
	}

	return true, map[string]interface{}{
		"status": 1,
		"trails": map[string]interface{}{
			"status":         1,
			"trails_removed": trailsRemoved,
		},
		"steps": map[string]interface{}{
			"status":        1,
			"steps_removed": stepsRemoved,
		},
		"objects": map[string]interface{}{
			"status":          1,
			"objects_removed": objectsRemoved,
		},
	}
}

// Validate : Validate PUT devices/{id} request
func (device *Device) Validate(r *http.Request) (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	params := mux.Vars(r)
	if !bson.IsObjectIdHex(params["id"]) {
		errs.Add("id", "Invalid Document ID"+"[ID:"+params["id"]+"]")
		return false, errs
	}
	device.ID = bson.ObjectIdHex(params["id"])
	db := db.Session
	err := db.C("pantahub_devices").Find(bson.M{"_id": device.ID}).One(&device)
	if err != nil {
		errs.Add("id", "Document ID not found"+"[ID:"+device.ID.Hex()+"]")
		return false, errs
	}
	return true, nil
}

// MergeErrors : Merge 2 arrays of Errors
func MergeErrors(errors url.Values, newErrors url.Values) url.Values {
	if len(newErrors) > 0 {
		for key, v := range newErrors {
			errors.Add(key, v[0])
		}
	}
	return errors
}

// GetGarbageRemovalTime : Get Garbage Removal Time
func GetGarbageRemovalTime() (time.Time, url.Values) {
	errors := url.Values{}
	RemoveGarbageAfter := utils.GetEnv("PANTAHUB_GC_GARBAGE_EXPIRY")
	parsedDuration, err := duration.FromString(RemoveGarbageAfter)
	if err != nil {
		errors.Add("parsing_iso8601", err.Error())
	}
	GarbageRemovalAt := time.Now().Local().Add(parsedDuration.ToDuration())
	return GarbageRemovalAt, errors
}

// IsDeviceValid : to check if a DeviceID is valid or not
func IsDeviceValid(DeviceID bson.ObjectId) (
	result bool,
	errs url.Values,
) {
	errs = url.Values{}
	db := db.Session
	count, err := db.C("pantahub_devices").Find(bson.M{"_id": DeviceID}).Count()
	if err != nil {
		errs.Add("device", err.Error())
	}
	return (count == 1), errs
}
