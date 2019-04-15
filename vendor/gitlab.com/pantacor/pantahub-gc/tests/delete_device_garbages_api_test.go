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

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestDeleteDeviceGarbages : Test Delete Device Garbages
func TestDeleteDeviceGarbages(t *testing.T) {
	log.Print("Test:Delete Device Garbages ")
	setUpDeleteDeviceGarbages(t)
	//Case 1:Delete Device Garbages after creating 1 trail 1 object
	log.Print(" Case 1:Delete Device Garbages after creating 1 trail 1 object")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha := helpers.GenerateObjectSha()
	objectSha, object, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	trail, res := helpers.CreateTrail(t, device, true, objectSha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	helpers.ReUsedObjectsCount++ //objectSha1 is used in trail & step with rev=0
	_, res = helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Device Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.ReUsedObjectsCount-- // as trail get garbaged
	_, res = helpers.ProcessTrailGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Trail Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.ProcessStepGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Step Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	// Update garbage removal time to current time(Note: Errors are handled inside the function)
	helpers.UpdateObjectGarbageRemovalDate(t, object)            //update removal time to current time
	helpers.UpdateTrailGarbageRemovalDate(t, trail)              //update removal time to current time
	helpers.UpdateStepGarbageRemovalDate(t, trail.ID.Hex()+"-0") //update removal time to current time

	result, res := helpers.DeleteDeviceGarbages(t)
	expectedResult := map[string]interface{}{}
	if utils.GetEnv("PANTAHUB_GC_REMOVE_GARBAGE") == "true" {
		if res.StatusCode() != 200 {
			t.Errorf("Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		}
		expectedResult = map[string]interface{}{
			"objects": map[string]interface{}{
				"objects_removed": 1,
				"status":          1,
			},
			"status": 1,
			"steps": map[string]interface{}{
				"status":        1,
				"steps_removed": 1,
			},
			"trails": map[string]interface{}{
				"status":         1,
				"trails_removed": 1,
			},
		}
	} else {
		if res.StatusCode() != 501 {
			t.Errorf("Expected Response code:501 but got:" + strconv.Itoa(res.StatusCode()))
		}
		expectedResult = map[string]interface{}{
			"objects": map[string]interface{}{
				"objects_removed": 0,
				"status":          0,
			},
			"status": 0,
			"steps": map[string]interface{}{
				"status":        0,
				"steps_removed": 0,
			},
			"trails": map[string]interface{}{
				"status":         0,
				"trails_removed": 0,
			},
		}

	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 1:Passed")
	} else {
		helpers.DisplayCounters()
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownDeleteDeviceGarbages(t)
}

// TestDeleteDeviceGarbagesWhenNoGarbagesToDelete : Test delete device garbages when no garbages to Delete
func TestDeleteDeviceGarbagesWhenNoGarbagesToDelete(t *testing.T) {
	log.Print("Test:Test delete device garbages when no garbages to Delete ")
	setUpDeleteDeviceGarbages(t)
	//Case 1:Test delete device garbages when no garbages to Delete
	log.Print(" Case 2:Test delete device garbages when no garbages to Delete")

	result, res := helpers.DeleteDeviceGarbages(t)
	expectedResult := map[string]interface{}{}
	if utils.GetEnv("PANTAHUB_GC_REMOVE_GARBAGE") == "true" {
		if res.StatusCode() != 200 {
			t.Errorf("Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		}
		expectedResult = map[string]interface{}{
			"objects": map[string]interface{}{
				"objects_removed": 0,
				"status":          1,
			},
			"status": 1,
			"steps": map[string]interface{}{
				"status":        1,
				"steps_removed": 0,
			},
			"trails": map[string]interface{}{
				"status":         1,
				"trails_removed": 0,
			},
		}
	} else {
		if res.StatusCode() != 501 {
			t.Errorf("Expected Response code:501 but got:" + strconv.Itoa(res.StatusCode()))
		}
		expectedResult = map[string]interface{}{
			"objects": map[string]interface{}{
				"objects_removed": 0,
				"status":          0,
			},
			"status": 0,
			"steps": map[string]interface{}{
				"status":        0,
				"steps_removed": 0,
			},
			"trails": map[string]interface{}{
				"status":         0,
				"trails_removed": 0,
			},
		}
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
	tearDownDeleteDeviceGarbages(t)
}
func setUpDeleteDeviceGarbages(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownDeleteDeviceGarbages(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
