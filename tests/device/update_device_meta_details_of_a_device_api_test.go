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

// TestUpdateDeviceMetaDetails : Test Update Device Meta Details Of A Device
func TestUpdateDeviceMetaDetails(t *testing.T) {
	connectToDb(t)
	setUpUpdateDeviceMetaDetails(t)
	log.Print("Test:Update Device Meta Details")
	t.Run("of valid device", testUpdateDeviceMetaDetailsOfValidDevice)
	t.Run("of invalid device", testUpdateDeviceMetaDetailsOfInvalidDevice)
	tearDownUpdateDeviceMetaDetails(t)
}

// testUpdateDeviceMetaDetailsOfValidDevice : Update Device Meta Details Of Valid Device
func testUpdateDeviceMetaDetailsOfValidDevice(t *testing.T) {
	log.Print(" Case 1:Update Device Meta Details Of Valid Device")
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
	result, res := helpers.LoginDevice(t, device.Prn, device.Secret)
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	dToken := result["token"].(string)
	deviceMetaDetails := map[string]interface{}{
		"name":  "test",
		"place": "berlin",
	}
	result, res = helpers.UpdateDeviceMetaDetails(
		t,
		dToken,
		device.ID.Hex(),
		deviceMetaDetails,
	)
	log.Print(result)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"name":  "test",
		"place": "berlin",
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

// testUpdateDeviceMetaDetailsOfInvalidDevice : test Update Device Meta Details Of Invalid Device
func testUpdateDeviceMetaDetailsOfInvalidDevice(t *testing.T) {
	log.Print(" Case 2:Update Device Meta Details Of Invalid Device")
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
	result, res := helpers.LoginDevice(t, device.Prn, device.Secret)
	if res.StatusCode() != 200 {
		t.Errorf("Error Login Device Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	dToken := result["token"].(string)
	deviceMetaDetails := map[string]interface{}{
		"name":  "test",
		"place": "berlin",
	}
	result, res = helpers.UpdateDeviceMetaDetails(
		t,
		dToken,
		"5c4dcf7d80123b2f2c7e96e2", //invalid deviceID
		deviceMetaDetails,
	)
	if res.StatusCode() != 400 {
		t.Errorf("Expected Response code:400 Bad Request but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Error updating device user-meta: not found",
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
func setUpUpdateDeviceMetaDetails(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownUpdateDeviceMetaDetails(t *testing.T) bool {
	return true
}
