//
// Copyright 2018  Pantacor Ltd.
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
	"reflect"
	"strconv"
	"testing"

	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestChangeDeviceSecret : Test Change Device Secret
func TestChangeDeviceSecret(t *testing.T) {
	setUpChangeDeviceSecret(t)
	log.Print("Test:Change Device Secret")
	t.Run("of valid device", testChangeSecretOfValidDevice)
	t.Run("of invalid device", testChangeSecretOfInvalidDevice)
	tearDownChangeDeviceSecret(t)
}

// testChangeSecretOfValidDevice : test Change Secret Of Valid Device
func testChangeSecretOfValidDevice(t *testing.T) {
	log.Print(" Case 1:Change Secret Of a Valid Device")
	helpers.Login(t, "user1", "user1")
	device, _ := helpers.CreateDevice(t, true, "123")
	result, res := helpers.LoginDevice(t, device.Prn, device.Secret)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	_, ok := result["token"].(string)
	if !ok {
		t.Errorf("Expected string value as token,but got")
		log.Print(reflect.TypeOf(result["token"]))
	}
	expectedResult := map[string]interface{}{
		"token": result["token"].(string),
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
	log.Print(" Case 2: Change Secret Of Invalid Device")

	result, res := helpers.LoginDevice(t, "invalid prn", "invalid password")
	if res.StatusCode() != 401 {
		t.Errorf("Expected Response code:401 UnAuthorized but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Not Authorized",
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

	log.Print(" Case 3:Login Empty values")
	result, res = helpers.LoginDevice(t, "", "")
	if res.StatusCode() != 401 {
		t.Errorf("Expected Response code:401 UnAuthorized but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"Error": "Not Authorized",
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
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownChangeDeviceSecret(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
