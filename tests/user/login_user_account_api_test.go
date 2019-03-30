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

// TestLoginUserAccount : Test Logn User Account
func TestLoginUserAccount(t *testing.T) {
	setUpVerifyUserAccount(t)
	log.Print("Test:Login User Account")
	t.Run("With valid account credentials", testLoginValidAccount)
	t.Run("With invalid account credentials", testLoginInvalidAccount)
	tearDownVerifyUserAccount(t)
}

// testValidAccount : test Valid Account
func testLoginValidAccount(t *testing.T) {
	log.Print(" Case 1:Valid Account")
	// POST auth/accounts
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
	account := helpers.GetUser(t, "test@gmail.com")
	_, res = helpers.VerifyUserAccount(t, account)
	if res.StatusCode() != 200 {
		t.Errorf("Error Verifying User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.Login(t, "testnick", "testpassword")
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

// testInvalidAccount : test Invalid Account
func testLoginInvalidAccount(t *testing.T) {
	log.Print(" Case 2:Login Invalid Account")
	// POST auth/accounts
	result, res := helpers.Login(t, "wrongnick", "wrongpassword")

	if res.StatusCode() != 401 {
		t.Errorf("Expected Response code:401 UnAuthorized failed but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Not Authorized",
	}
	//"Error": "Invalid Challenge (wrong, used or never existed)",
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
	// POST auth/accounts
	result, res = helpers.Login(t, "", "")

	if res.StatusCode() != 401 {
		t.Errorf("Expected Response code:401 UnAuthorized failed but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"Error": "Not Authorized",
	}
	//"Error": "Invalid Challenge (wrong, used or never existed)",
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
func setUpLoginUserAccount(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownLoginUserAccount(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
