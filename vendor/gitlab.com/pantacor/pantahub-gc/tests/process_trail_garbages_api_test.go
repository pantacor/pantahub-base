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

// TestProcessTrailGarbagesWithInvalidStateAndNoObjects : Process trail garbages with invalid state & no objects
func TestProcessTrailGarbagesWithInvalidStateAndNoObjects(t *testing.T) {
	log.Print("Test:Process Trail Garbages")
	setUpProcessTrailGarbages(t)
	//Case1:Process trail garbages with invalid state & no objects
	log.Print(" Case 1:Process trail garbages with invalid state & no objects")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device, false, "")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Traile:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Device Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 400 {
		t.Errorf("Error Processing Trail Garbages(With Invalid States & No Objects):Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                    0,
		"objects_marked_as_garbage": 0,
		"objects_with_errors":       0,
		"objects_ignored":           0,
		"steps_marked_as_garbage":   0,
		"steps_with_errors":         0,
		"trails_processed":          0,
		"trails_with_errors":        helpers.InvalidTrailsCount,
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
	tearDownProcessTrailGarbages(t)
}

// TestProcessTrailGarbagesWithValidStatesAndObjects : Process trail garbages with valid states & valid objects
func TestProcessTrailGarbagesWithValidStatesAndObjects(t *testing.T) {
	setUpProcessTrailGarbages(t)
	// Case2:Process trail garbages with states & objects
	log.Print(" Case 2:Process trail garbages with valid states & valid objects")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha := helpers.GenerateObjectSha()
	objectSha, _, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device, true, objectSha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount++ // objectSha will be reused in step rev=0

	_, res = helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Device Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount-- // Decrement the object reuse counter as the trail becomes garbage
	result, res := helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Trail Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	expectedResult := map[string]interface{}{
		"status":                    1,
		"objects_marked_as_garbage": helpers.ObjectsCount,
		"objects_with_errors":       0,
		"objects_ignored":           0,
		"steps_marked_as_garbage":   helpers.StepsCount,
		"steps_with_errors":         0,
		"trails_processed":          helpers.TrailsCount,
		"trails_with_errors":        0,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 2:Passed")
	} else {
		helpers.DisplayCounters()
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownProcessTrailGarbages(t)

}

// TestProcessTrailGarbagesWithInValidObjects : Process trail garbages with invalid objects
func TestProcessTrailGarbagesWithInValidObjects(t *testing.T) {
	setUpProcessTrailGarbages(t)
	// Case 3:Process trail garbages with invalid objects
	log.Print(" Case 3:Process trail garbages with invalid objects")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	objectSha := helpers.GenerateObjectSha() //invalid object
	helpers.InvalidObjectsCount++
	_, res = helpers.CreateTrail(t, device, true, objectSha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.InvalidTrailsCount++
	helpers.InvalidStepsCount++ // for step(rev=0)

	_, res = helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Device Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	result, res := helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 400 {
		t.Errorf("Error Processing Trail Garbages(with Invalid Objects):Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                    0,
		"objects_marked_as_garbage": 0,
		"objects_with_errors":       helpers.InvalidObjectsCount,
		"objects_ignored":           0,
		"steps_marked_as_garbage":   (helpers.StepsCount - helpers.InvalidStepsCount),
		"steps_with_errors":         helpers.InvalidStepsCount,
		"trails_processed":          (helpers.TrailsCount - helpers.InvalidTrailsCount),
		"trails_with_errors":        helpers.InvalidTrailsCount,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 3:Passed")
	} else {
		helpers.DisplayCounters()
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownProcessTrailGarbages(t)
}

// TestProcessTrailGarbagesWithReUsedObjects : Process trail garbages with reused objects
func TestProcessTrailGarbagesWithReUsedObjects(t *testing.T) {
	setUpProcessTrailGarbages(t)
	// Case 4:Process trail garbages with  reused objects)
	log.Print(" Case 4:Process trail garbages with reused objects in 3 different trails")
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
	device3, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha := helpers.GenerateObjectSha()
	objectSha1, _, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device1, true, objectSha1) // trail 1
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device2, true, objectSha1) // trail 2
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device3, true, objectSha1) // trail 2
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount++ //objectSha1 is used in 3 trails & step with rev=0
	_, res = helpers.PopulateTrailsUsedObjects(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Populating Trails Used Objects:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.PopulateStepsUsedObjects(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Populating Steps Used Objects:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.MarkDeviceAsGarbage(t, device1)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.MarkDeviceAsGarbage(t, device2) //Note:device3 we are not marking as garbage
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessDeviceGarbages(t) //trail 1 & 2 becomes garbage
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Device Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Trail Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	//helpers.ReUsedObjectsCount-- // Don't use this  here as the object is still in use inside trail3&step3(rev=0),|| Decrement the object reuse counter as the trails & steps becomes garbage
	expectedResult := map[string]interface{}{
		"status":                    1,
		"objects_marked_as_garbage": 0,
		"objects_with_errors":       0,
		"objects_ignored":           1,
		"steps_marked_as_garbage":   2,
		"steps_with_errors":         0,
		"trails_processed":          2,
		"trails_with_errors":        0,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 4:Passed")
	} else {
		helpers.DisplayCounters()
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownProcessTrailGarbages(t)
}
func setUpProcessTrailGarbages(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user1/user1 & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownProcessTrailGarbages(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
