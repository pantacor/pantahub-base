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
	"strconv"
	"testing"
	"time"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-gc/models"
	"gopkg.in/mgo.v2/bson"
)

// PopulateStepsUsedObjects : Populate Steps used_objects field
func PopulateStepsUsedObjects(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := gCAPIUrl + "/populate/usedobjects/steps"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// PopulateStepUsedObjects : Populate Step used_objects field
func PopulateStepUsedObjects(t *testing.T, stepID string) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := gCAPIUrl + "/populate/usedobjects/steps/" + stepID
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// ProcessStepGarbages : Process Step Garbages
func ProcessStepGarbages(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := gCAPIUrl + "/processgarbages/steps"
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// CreateStep : Create new Step under a trail
func CreateStep(
	t *testing.T,
	device models.Device,
	revision int,
	includeState bool,
	objectSha string,
) (
	models.Step,
	*resty.Response,
) {
	// get DTOKEN by doing device login
	loginResponse, _ := LoginDevice(t, device.Prn, device.Secret)
	DTOKEN := ""
	dtoken, ok := loginResponse["token"].(string)
	if ok {
		DTOKEN = dtoken
	}
	// Set DTOKEN in the Request object
	request := resty.R().SetAuthToken(DTOKEN)
	// Add State values
	if includeState {
		request = request.SetBody(map[string]interface{}{
			"rev":        revision,
			"commit-msg": "Commit for Revision:" + strconv.Itoa(revision),
			"state": map[string]interface{}{
				"#spec":  "pantavisor-multi-platform@1",
				"kernel": objectSha,
			},
		})
	} else {
		InvalidStepsCount++
	}
	APIEndPoint := BaseAPIUrl + "/trails/" + device.ID.Hex() + "/steps"
	res, err := request.Post(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	step := models.Step{}
	err = json.Unmarshal(res.Body(), &step)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	StepsCount++
	return step, res
}

// ListSteps : List Steps of a trail
func ListSteps(
	t *testing.T,
	trailID string,
) (
	[]interface{},
	*resty.Response,
) {
	response := []interface{}{}
	APIEndPoint := BaseAPIUrl + "/trails/" + trailID + "/steps"
	request := resty.R().SetAuthToken(UTOKEN)
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

// DeleteAllSteps : Delete All Steps from the database
func DeleteAllSteps(t *testing.T) bool {
	db := db.Session
	c := db.C("pantahub_steps")
	_, err := c.RemoveAll(bson.M{})
	if err != nil {
		t.Errorf("Error on Removing: " + err.Error())
		t.Fail()
		return false
	}
	return true
}

// UpdateStepGarbageRemovalDate : Update Step Garbage Removal Date
func UpdateStepGarbageRemovalDate(t *testing.T, stepID string) bool {
	GarbageRemovalAt := time.Now().Local().Add(-time.Minute * time.Duration(1)) //decrease 1 min
	db := db.Session
	c := db.C("pantahub_steps")
	err := c.Update(
		bson.M{"_id": stepID},
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

// UpdateStepProgress : Update Step Progress
func UpdateStepProgress(
	t *testing.T,
	trailID string,
	step string,
	dtoken string,
	progressData map[string]interface{},
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/trails/" + trailID + "/steps/" + step + "/progress"
	res, err := resty.R().
		SetBody(progressData).
		SetAuthToken(dtoken).
		Put(APIEndPoint)
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

// GetStep : Get Step Details
func GetStep(
	t *testing.T,
	trailID string,
	step string,
) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/trails/" + trailID + "/steps/" + step
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		Get(APIEndPoint)
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
