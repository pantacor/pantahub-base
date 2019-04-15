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

// TestMarkTrailAsGarbage : Mark Trails as Garbages that lost their parent devices
func TestMarkTrailAsGarbageThatlostParentDevice(t *testing.T) {
	setUpMarkTrailAsGarbage(t)
	log.Print("Test:Mark Trail as Garbages that lost their parent device")
	// Case 1 : Mark trails as garbage that lost their parent device
	log.Print(" Case 1:Mark trails as garbage that lost their parent device")
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
	helpers.DeleteAllDevices(t)
	expectedResult := map[string]interface{}{
		"status":        1,
		"trails_marked": helpers.TrailsCount,
	}
	result, res := helpers.MarkTrailsAsGarbage(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Trail As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 1:Passed")
	} else {
		log.Print("Expected:")
		log.Print(expectedResult)
		log.Print("But Got:")
		log.Print(result)
		t.Fail()
	}
	helpers.ClearOldData(t)
	tearDownMarkTrailAsGarbage(t)
}

// TestMarkTrailAsGarbageThatHaveValidParentDevice : Mark Trails as Garbages that lost their parent devices
func TestMarkTrailAsGarbageThatHaveValidParentDevice(t *testing.T) {
	setUpMarkTrailAsGarbage(t)
	log.Print("Test:Mark Trails as Garbages that lost their parent devices")
	//Case 2:Mark trail as garbage which have valid parent device
	log.Print(" Case 2:Mark trail as garbage which have valid parent device")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
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
	result, res := helpers.MarkTrailsAsGarbage(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Trail As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":        1,
		"trails_marked": 0,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 2:Passed")
	} else {
		log.Print("Expected:")
		log.Print(expectedResult)
		log.Print("But Got:")
		log.Print(result)
		t.Fail()
	}
	tearDownMarkTrailAsGarbage(t)
}
func setUpMarkTrailAsGarbage(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownMarkTrailAsGarbage(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
