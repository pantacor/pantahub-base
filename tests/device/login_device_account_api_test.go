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
	"reflect"
	"strconv"
	"testing"

	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestLoginDeviceAccount : Test Logn Device Accountcd
func TestLoginDeviceAccount(t *testing.T) {
	connectToDb(t)
	setUpLoginDeviceAccount(t)
	log.Print("Test:Login User Account")
	t.Run("With valid device account credentials", testLoginValidDeviceAccount)
	t.Run("With invalid device account credentials", testLoginInvalidDeviceAccount)
	tearDownLoginDeviceAccount(t)
}

// testValidDeviceAccount : test Valid Device Account
func testLoginValidDeviceAccount(t *testing.T) {
	log.Print(" Case 1:Valid Device Account")
	// Register user account
	_, res := helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	if res.StatusCode() != 200 {
		t.Errorf("Error Register User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	account := helpers.GetUser(t, "test@gmail.com", MongoDb)
	_, res = helpers.VerifyUserAccount(t, account.Id.Hex(), account.Challenge)
	if res.StatusCode() != 200 {
		t.Errorf("Error Verifying User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.Login(t, "testnick", "testpassword")
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

// testLoginInvalidDeviceAccount : test Login Invalid Device Account
func testLoginInvalidDeviceAccount(t *testing.T) {
	log.Print(" Case 2: Login Invalid Device Account")

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
func setUpLoginDeviceAccount(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownLoginDeviceAccount(t *testing.T) bool {
	return true
}
