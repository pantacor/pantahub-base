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

// Test : Test Claim Ownership Of Device
func TestClaimOwnershipOfDevice(t *testing.T) {
	connectToDb(t)
	setUpClaimOwnershipOfDevice(t)
	log.Print("Test:Assign User To Device")
	t.Run("to valid device", testClaimValidDevice)
	t.Run("With invalid device", testClaimInvalidDevice)
	tearDownClaimOwnershipOfDevice(t)
}

// testClaimValidDevice : test Claim Valid Device
func testClaimValidDevice(t *testing.T) {
	log.Print(" Case 1:Claim Valid Device ")
	// Register user account
	_, res := helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	if res.StatusCode() != 200 {
		t.Errorf("Error Registering User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	user := helpers.GetUser(t, "test@gmail.com", MongoDb) //Error handled inside the function
	_, res = helpers.VerifyUserAccount(t, user.Id.Hex(), user.Challenge)
	if res.StatusCode() != 200 {
		t.Errorf("Error Verifying User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.Login(t, "testnick", "testpassword")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	device, res := helpers.CreateDevice(t, false, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
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

// testClaimInvalidDevice : test Claim Invalid Device
func testClaimInvalidDevice(t *testing.T) {
	log.Print(" Case 2:Claim Invalid Device ")
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

	log.Print(" Case 3:Claim valid device with Invalid Challenge ")

	device, res := helpers.CreateDevice(t, false, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
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
func setUpClaimOwnershipOfDevice(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownClaimOwnershipOfDevice(t *testing.T) bool {
	return true
}
