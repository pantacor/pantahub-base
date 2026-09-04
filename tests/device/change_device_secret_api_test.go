// Copyright 2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package tests

import (
	"log"
	"strconv"
	"testing"

	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestChangeDeviceSecret : the device secret is generated once at registration
// and can never be changed through PUT. A PUT carrying a secret (or an empty
// one) must succeed without altering or echoing it.
func TestChangeDeviceSecret(t *testing.T) {
	connectToDb(t)
	setUpChangeDeviceSecret(t)
	log.Print("Test:Change Device Secret")
	t.Run("of valid device is ignored", testChangeSecretOfValidDevice)
	t.Run("of invalid device", testChangeSecretOfInvalidDevice)
	t.Run("to empty", testChangeSecretToEmpty)
	tearDownChangeDeviceSecret(t)
}

// testChangeSecretOfValidDevice : a secret in the PUT body is ignored, not stored
func testChangeSecretOfValidDevice(t *testing.T) {
	log.Print(" Case 1:Change Secret Of a Valid Device is ignored")
	helpers.Login(t, "user1", "user1")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("%s", "Error Creating Device:Expected Response code:200 but got:"+strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.ChangeDeviceSecret(t, device.ID.Hex(), "NewSecret")
	if res.StatusCode() != 200 {
		t.Errorf("%s", "Expected Response code:200 OK but got:"+strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"id":    device.ID.Hex(),
		"prn":   device.Prn,
		"nick":  device.Nick,
		"owner": device.Owner,
	}
	if _, present := result["secret"]; present {
		t.Errorf("secret must not be echoed back from PUT, got: %v", result["secret"])
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

// testChangeSecretOfInvalidDevice : test Change Secret Of Invalid Device
func testChangeSecretOfInvalidDevice(t *testing.T) {
	log.Print(" Case 2:Change Secret Of Invalid Device")
	helpers.Login(t, "user1", "user1")
	result, res := helpers.ChangeDeviceSecret(t, "5c4dcf7d80123b2f2c7e96e2", "NewSecret")
	if res.StatusCode() != 403 {
		t.Errorf("%s", "Expected Response code:403 Forbidden but got:"+strconv.Itoa(res.StatusCode()))
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

// testChangeSecretToEmpty : an empty secret in PUT is accepted (claim bodies are "{}")
func testChangeSecretToEmpty(t *testing.T) {
	log.Print(" Case 3:Change Device Secret to empty is accepted")
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("%s", "Error Creating Device:Expected Response code:200 but got:"+strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.ChangeDeviceSecret(t, device.ID.Hex(), "")
	if res.StatusCode() != 200 {
		t.Errorf("%s", "Expected Response code:200 OK but got:"+strconv.Itoa(res.StatusCode()))
	}
	if _, present := result["secret"]; present {
		t.Errorf("secret must not be echoed back from PUT, got: %v", result["secret"])
	}
	expectedResult := map[string]interface{}{
		"id":  device.ID.Hex(),
		"prn": device.Prn,
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
func setUpChangeDeviceSecret(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownChangeDeviceSecret(t *testing.T) bool {
	return true
}
