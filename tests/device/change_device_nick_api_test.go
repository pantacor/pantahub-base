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

	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// ChangeDeviceNick : Change Device Nick
func TestChangeDeviceNick(t *testing.T) {
	setUpChangeDeviceNick(t)
	log.Print("Test:Change Device Nick")
	t.Run("of valid device", testChangeDeviceNickOfValidDevice)
	t.Run("of invalid device", testChangeDeviceNickOfInvalidDevice)
	tearDownChangeDeviceNick(t)
}

// testChangeDeviceNickOfValidDevice : test Change Device Nick Of a Valid Device
func testChangeDeviceNickOfValidDevice(t *testing.T) {
	log.Print(" Case 1:Change Device Nick Of a Valid Device")
	helpers.Login(t, "user1", "user1")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.UpdateDeviceNick(t, device.ID.Hex(), "newNick")
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"id":    device.ID.Hex(),
		"prn":   device.Prn,
		"nick":  "newNick",
		"owner": device.Owner,
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

	log.Print(" Case 2:Set Empty Nick Name")
	result, res = helpers.UpdateDeviceNick(t, device.ID.Hex(), "")
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"id":    device.ID.Hex(),
		"prn":   device.Prn,
		"nick":  "newNick",
		"owner": device.Owner,
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

// testChangeDeviceNickOfInvalidDevice : test Change Device Nick Of An Invalid Device
func testChangeDeviceNickOfInvalidDevice(t *testing.T) {
	log.Print(" Case 3:Change Device Nick Of An Invalid Device")
	helpers.Login(t, "user1", "user1")
	result, res := helpers.UpdateDeviceNick(t, "5c4dcf7d80123b2f2c7e96e2", "NewNick")
	if res.StatusCode() != 403 {
		t.Errorf("Expected Response code:403 Forbidden but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Not Accessible Resource Id",
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 3:Passed")
	} else {
		log.Print(" Case 3:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
}
func setUpChangeDeviceNick(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownChangeDeviceNick(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
