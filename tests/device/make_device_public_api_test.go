//
// Copyright 2019  Pantacor Ltd.
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

	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestMakeDeviceNonPublic : Test Make Device Non Public
func TestMakeDeviceNonPublic(t *testing.T) {
	connectToDb(t)
	setUpMakeDeviceNonPublic(t)
	log.Print("Test:Make Device Non Public")
	t.Run("of valid device", testMakeValidDeviceNonPublic)
	t.Run("of invalid device", testMakeInvalidDeviceNonPublic)
	tearDownMakeDeviceNonPublic(t)
}

// testMakeValidDeviceNonPublic : test Make Valid Device Non Public
func testMakeValidDeviceNonPublic(t *testing.T) {
	log.Print(" Case 1:Make Device Non Public")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.MakeDevicePublic(t, device.ID.Hex())
	result, res := helpers.MakeDeviceNonPublic(t, device.ID.Hex())
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"id":     device.ID.Hex(),
		"prn":    device.Prn,
		"nick":   device.Nick,
		"owner":  device.Owner,
		"secret": device.Secret,
		"public": false,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 1:Passed")
	} else {
		log.Print(" Case 1:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}

}

// testMakeInvalidDeviceNonPublic : test Make Invalid Device Non Public
func testMakeInvalidDeviceNonPublic(t *testing.T) {
	log.Print(" Case 2:Make Invalid Device Non Public")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.MakeDeviceNonPublic(t, "5c4dcf7d80123b2f2c7e96e2")
	if res.StatusCode() != 403 {
		t.Errorf("Expected Response code:403 Forbidden but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Not Accessible Resource Id",
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 2:Passed")
	} else {
		log.Print(" Case 2:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
}
func setUpMakeDeviceNonPublic(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownMakeDeviceNonPublic(t *testing.T) bool {
	return true
}
