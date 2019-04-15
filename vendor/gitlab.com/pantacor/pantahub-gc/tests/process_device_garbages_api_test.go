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

// TestProcessDeviceGarbagesWithNoTrails : Processing garbage devices with no trails
func TestProcessDeviceGarbagesWithNoTrails(t *testing.T) {
	setUpProcessDeviceGarbages(t)
	log.Print("Test:Process Device Garbages")
	// Case 1:Processing garbage devices with no trails
	log.Print(" Case 1:Processing garbage devices with no trails")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 400 {
		t.Errorf("Error Process Device Garbages(with no trails):Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                   0,
		"device_processed":         0,
		"trails_marked_as_garbage": 0,
		"trails_with_errors":       helpers.DevicesCount,
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
	tearDownProcessDeviceGarbages(t)
}

// TestProcessDeviceGarbagesWithTrails : Processing garbage devices with trails
func TestProcessDeviceGarbagesWithTrails(t *testing.T) {
	setUpProcessDeviceGarbages(t)
	// Case 2:Processing garbage devices with trails
	log.Print(" Case 2:Processing garbage devices with trails")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device, false, "")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	helpers.MarkDeviceAsGarbage(t, device)
	result, res := helpers.ProcessDeviceGarbages(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Processing Device Garbages:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":                   1,
		"device_processed":         helpers.DevicesCount,
		"trails_marked_as_garbage": helpers.TrailsCount,
		"trails_with_errors":       0,
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
	tearDownProcessDeviceGarbages(t)
}
func setUpProcessDeviceGarbages(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownProcessDeviceGarbages(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
