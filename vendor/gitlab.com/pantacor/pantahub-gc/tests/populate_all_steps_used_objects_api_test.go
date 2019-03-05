//
// Copyright 2018-2019  Pantacor Ltd.
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
package tests

import (
	"log"
	"strconv"
	"testing"

	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestPopulateAllStepsUsedObjectsWithInvalidObjects : Populate steps used_objects field with state having invalid objects in it
func TestPopulateAllStepsUsedObjectsWithInvalidObjects(t *testing.T) {
	log.Print("Test:Populate Step used_objects field ")
	setUpPopulateAllStepsUsedObject(t)
	//Case1:Process step garbages with invalid state & no objects
	log.Print(" Case 1:Populate step used_objects field with state having invalid objects in it")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	objectSha := helpers.GenerateObjectSha() //invalid object
	_, res = helpers.CreateTrail(t, device, true, objectSha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.CreateStep(t, device, 1, true, objectSha) //adding new step
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.PopulateTrailsUsedObjects(t)
	if res.StatusCode() != 400 {
		t.Errorf("Error Populate Trails Used Objects(with Invalid Objects):Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.PopulateStepsUsedObjects(t)
	if res.StatusCode() != 400 {
		t.Errorf("Error Populate Steps Used Objects(with Invalid Objects):Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	result, res := helpers.PopulateStepsUsedObjects(t)
	if res.StatusCode() != 400 {
		t.Errorf("Error Populate Steps Used Objects(using invalid objects):Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":            0,
		"steps_populated":   0,
		"steps_with_errors": helpers.StepsCount,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 1:Passed")
	} else {
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownPopulateAllStepsUsedObject(t)
}

// TestPopulateAllStepsUsedObjectsWithValidObjects : Populate steps used_objects field with state having valid objects in it
func TestPopulateAllStepsUsedObjectsWithValidObjects(t *testing.T) {
	log.Print("Test:Populate all Steps used_objects field ")
	setUpPopulateAllStepsUsedObject(t)
	//Case1:Process trail garbages with valid state & no objects
	log.Print(" Case 2:Populate steps used_objects field with state having valid objects in it")
	device1, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device2, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha := helpers.GenerateObjectSha()
	objectSha1, _, res := helpers.CreateObject(t, sha) //valid object
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	_, res = helpers.CreateTrail(t, device1, true, objectSha1)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount++ // objectSha1 will be reused in step rev=0

	sha = helpers.GenerateObjectSha()
	objectSha2, _, res := helpers.CreateObject(t, sha) //valid object
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateStep(t, device1, 1, true, objectSha2) //adding new step
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha = helpers.GenerateObjectSha()
	objectSha3, _, res := helpers.CreateObject(t, sha) //valid object
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	_, res = helpers.CreateTrail(t, device2, true, objectSha3)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount++ // objectSha3 will be reused in step rev=0

	sha = helpers.GenerateObjectSha()
	objectSha4, _, res := helpers.CreateObject(t, sha) //valid object
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.CreateStep(t, device2, 1, true, objectSha4) //adding new step

	result, res := helpers.PopulateStepsUsedObjects(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Populating Steps Used Objects:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":            1,
		"steps_populated":   4,
		"steps_with_errors": 0,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 2:Passed")
	} else {
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownPopulateAllStepsUsedObject(t)
}
func setUpPopulateAllStepsUsedObject(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownPopulateAllStepsUsedObject(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
