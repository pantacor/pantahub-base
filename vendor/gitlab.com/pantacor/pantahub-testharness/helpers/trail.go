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
package helpers

import (
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-gc/models"
	"gopkg.in/mgo.v2/bson"
)

var gCAPIUrl = utils.GetEnv("PANTAHUB_GC_API")

// PopulateTrailsUsedObjects : Populate Trails used_objects field
func PopulateTrailsUsedObjects(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := gCAPIUrl + "/populate/usedobjects/trails"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// PopulateTrailUsedObjects : Populate Trail used_objects field
func PopulateTrailUsedObjects(t *testing.T, trailID string) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := gCAPIUrl + "/populate/usedobjects/trails/" + trailID
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// ProcessTrailGarbages : Process Device Garbages
func ProcessTrailGarbages(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := gCAPIUrl + "/processgarbages/trails"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// CreateTrail : Create a trail
func CreateTrail(
	t *testing.T,
	device models.Device,
	includeState bool,
	objectSha string,
) (
	models.Trail,
	*resty.Response,
) {
	APIEndPoint := BaseAPIUrl + "/trails/"
	loginResponse, _ := LoginDevice(t, device.Prn, device.Secret)
	DTOKEN := ""
	dtoken, ok := loginResponse["token"].(string)
	if ok {
		DTOKEN = dtoken
	}
	request := resty.R().SetAuthToken(DTOKEN)
	if includeState {
		//this objectSha will be reused in step(rev=0)
		request = request.SetBody(map[string]string{
			"#spec":  "pantavisor-multi-platform@1",
			"kernel": objectSha,
		})
	} else {
		InvalidTrailsCount++
		InvalidStepsCount++
	}
	res, err := request.Post(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	trail := models.Trail{}
	err = json.Unmarshal(res.Body(), &trail)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	TrailsCount++
	StepsCount++
	return trail, res
}

// DeleteAllTrails : Delete All Trails
func DeleteAllTrails(t *testing.T) bool {
	db := db.Session
	c := db.C("pantahub_trails")
	_, err := c.RemoveAll(bson.M{})
	if err != nil {
		t.Errorf("Error on Removing trail: " + err.Error())
		t.Fail()
		return false
	}
	Trails = []models.Trail{}
	return true
}

// DeleteTrail : Delete a Trail
func DeleteTrail(t *testing.T, trail models.Trail) bool {
	db := db.Session
	c := db.C("pantahub_trails")
	err := c.Remove(bson.M{"_id": trail.ID})
	if err != nil {
		t.Errorf("Error on Removing trail: " + err.Error())
		t.Fail()
		return false
	}
	return true
}

// MarkTrailsAsGarbage : Mark Trails as Garbages that lost their parent devices
func MarkTrailsAsGarbage(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := GCAPIUrl + "/markgarbage/trails"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if res.StatusCode() != 200 {
		log.Print(response)
		t.Fail()
	}
	return response, res
}

// UpdateTrailGarbageRemovalDate : Update Trail Garbage Removal Date
func UpdateTrailGarbageRemovalDate(t *testing.T, trail models.Trail) bool {
	GarbageRemovalAt := time.Now().Local().Add(-time.Minute * time.Duration(1)) //decrease 1 min
	db := db.Session
	c := db.C("pantahub_trails")
	err := c.Update(
		bson.M{"_id": trail.ID},
		bson.M{"$set": bson.M{
			"garbage_removal_at": GarbageRemovalAt,
		}})
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
		return false
	}
	return true
}

// ListTrails : List Trails Of A Device
func ListTrails(
	t *testing.T,
	deviceID string,
	dToken string,
) (
	[]interface{},
	*resty.Response,
) {
	response := []interface{}{}
	APIEndPoint := BaseAPIUrl + "/trails/"
	request := resty.R().SetAuthToken(dToken)
	res, err := request.Get(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}
