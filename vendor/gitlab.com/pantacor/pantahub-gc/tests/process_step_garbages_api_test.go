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

// TestProcessDefaultStepGarbages : Process step(rev=0) garbages with valid state & object
func TestProcessDefaultStepGarbages(t *testing.T) {
	log.Print("Test:Process Step Garbages")
	setUpProcessStepGarbages(t)
	//Case1:Process step(rev=0) garbages with valid state & object
	log.Print(" Case 1:Process step(rev=0) garbages with valid state & objects")
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
	helpers.ReUsedObjectsCount++ //objectSha is used in 1 trails &  1 step with rev=0

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

	helpers.ProcessTrailGarbages(t)
	result, res := helpers.ProcessStepGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Step Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                    1,
		"steps_processed":           1,
		"objects_marked_as_garbage": 1,
		"objects_with_errors":       0,
		"objects_ignored":           0,
		"steps_with_errors":         0,
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
	tearDownProcessStepGarbages(t)
}

// TestProcessMultipleStepGarbagesWithMoreRevisions : Process step(rev=0&rev=1&rev=2) garbages with valid state & objects
func TestProcessMultipleStepGarbagesWithMoreRevisions(t *testing.T) {
	setUpProcessStepGarbages(t)
	//Case2:Process step(rev=0&rev=1&rev=2) garbages with valid state & objects
	log.Print(" Case 2:Process step(rev=0&rev=1&rev=2) garbages with valid state & objects")
	device, res := helpers.CreateDevice(t, true, "123")
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
	_, res = helpers.CreateTrail(t, device, true, objectSha1)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount++ //objectSha1 is used in 1 trail &  1 step with rev=0
	sha = helpers.GenerateObjectSha()
	objectSha2, _, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	//Add step1
	_, res = helpers.CreateStep(t, device, 1, true, objectSha2) //rev=1
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha = helpers.GenerateObjectSha()
	objectSha3, _, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	//Add step2
	_, res = helpers.CreateStep(t, device, 2, true, objectSha3) //rev=2
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
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
	helpers.ReUsedObjectsCount-- // Decrement the object reuse counter as the trail becomes garbage

	_, res = helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Trail Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.ProcessStepGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Step Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                    1,
		"steps_processed":           3,
		"objects_marked_as_garbage": 3,
		"objects_with_errors":       0,
		"objects_ignored":           0,
		"steps_with_errors":         0,
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
	tearDownProcessStepGarbages(t)
}

// TestProcessMultipleStepGarbagesWithReUsedObjects : Process step(rev=0&rev=1&rev=2) garbages with valid state & reused objects
func TestProcessMultipleStepGarbagesWithReUsedObjects(t *testing.T) {
	setUpProcessStepGarbages(t)
	//Case3:Process step(rev=0&rev=1&rev=2) garbages with valid state & reused objects
	log.Print(" Case 3:Process step(rev=0&rev=1&rev=2) garbages with valid state & reused objects")
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
	_, res = helpers.CreateStep(t, device, 1, true, objectSha) //rev=1,note:using the same object here
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateStep(t, device, 2, true, objectSha) //rev=2,note:using the same object here
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount++ //objectSha is used in trail & step with rev=0,1&2

	_, res = helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Procesing Device Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Trail Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount-- // Decrement the object reuse counter as the trail becomes garbage

	result, res := helpers.ProcessStepGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Step Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                    1,
		"steps_processed":           3,
		"objects_marked_as_garbage": 1,
		"objects_with_errors":       0,
		"objects_ignored":           0,
		"steps_with_errors":         0,
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
	tearDownProcessStepGarbages(t)

}
func setUpProcessStepGarbages(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownProcessStepGarbages(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
