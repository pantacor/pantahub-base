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

// TestUpdateUserMetaDetails : Test Update User Meta Details Of A Device
func TestUpdateUserMetaDetails(t *testing.T) {
	connectToDb(t)
	setUpUpdateUserMetaDetails(t)
	log.Print("Test:Update User Meta Details")
	t.Run("of valid device", testUpdateUserMetaDetailsOfValidDevice)
	t.Run("of invalid device", testUpdateUserMetaDetailsOfInvalidDevice)
	tearDownUpdateUserMetaDetails(t)
}

// testUpdateUserMetaDetailsOfValidDevice : Update User Meta Details Of Valid Device
func testUpdateUserMetaDetailsOfValidDevice(t *testing.T) {
	log.Print(" Case 1:Update User Meta Details Of Valid Device")
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
	userMetaDetails := map[string]interface{}{
		"name":  "test",
		"place": "berlin",
	}
	result, res := helpers.UpdateUserMetaDetails(t, device.ID.Hex(), userMetaDetails)
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

// testUpdateUserMetaDetailsOfInvalidDevice : test Update User Meta Details Of Invalid Device
func testUpdateUserMetaDetailsOfInvalidDevice(t *testing.T) {
	log.Print(" Case 2:Update User Meta Details Of Invalid Device")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	userMetaDetails := map[string]interface{}{
		"name":  "test",
		"place": "berlin",
	}
	result, res := helpers.UpdateUserMetaDetails(t, "5c4dcf7d80123b2f2c7e96e2", userMetaDetails)
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
func setUpUpdateUserMetaDetails(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownUpdateUserMetaDetails(t *testing.T) bool {
	return true
}
