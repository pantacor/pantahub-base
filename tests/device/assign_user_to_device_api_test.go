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

// TestAssignUserToDevice : Test Assign User To Device
func TestAssignUserToDevice(t *testing.T) {
	setUpAssignUserToDevice(t)
	log.Print("Test:Assign User To Device")
	t.Run("to valid device", testAssignToValidDevice)
	t.Run("With invalid device", testAssignToInvalidDevice)
	tearDownAssignUserToDevice(t)
}

// testAssignToValidDevice : test Assign To Valid Device
func testAssignToValidDevice(t *testing.T) {
	log.Print(" Case 1:Assign User To Valid Device ")
	// Register user account
	helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	user := helpers.GetUser(t, "test@gmail.com")
	helpers.VerifyUserAccount(t, user)
	helpers.Login(t, "testnick", "testpassword")

	device, _ := helpers.CreateDevice(t, false, "123")
	result, res := helpers.AssignUserToDevice(t, device.ID.Hex(), device.Challenge)

	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"id":     device.ID.Hex(),
		"prn":    device.Prn,
		"nick":   device.Nick,
		"owner":  user.Prn,
		"secret": device.Secret,
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

// testAssignToInvalidDevice : test Assign To Invalid Device
func testAssignToInvalidDevice(t *testing.T) {
	log.Print(" Case 2:Assign User To Invalid Device ")
	//assigning user to invalid device
	result, res := helpers.AssignUserToDevice(t, "5c4dcf7d80123b2f2c7e96e2", "invalid_challenge")

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

	log.Print(" Case 3:Assign User To valid device with Invalid Challenge ")

	device, _ := helpers.CreateDevice(t, false, "123")
	//make challenge invalid
	device.Challenge = "invalid_challenge"
	result, res = helpers.AssignUserToDevice(t, device.ID.Hex(), device.Challenge)
	if res.StatusCode() != 403 {
		t.Errorf("Expected Response code:403 Forbidden but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"Error": "No Access to Device",
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
func setUpAssignUserToDevice(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownAssignUserToDevice(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
