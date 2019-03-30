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
	"strconv"
	"testing"

	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// testRegisterUserAccount : Test Register User Account
func TestRegisterUserAccount(t *testing.T) {
	setUpUserRegistration(t)
	log.Print("Test:Register User Account account")
	t.Run("With all required data", testWithData)
	t.Run("With empty data", testWithEmptyData)
	t.Run("Email Uniqueness", testEmailUniqueness)
	t.Run("Nick Uniqueness", testNickUniqueness)
	tearDownUserRegistration(t)
}

// testRegisterUserAccountWithData : Test Register User Account with all required data
func testWithData(t *testing.T) {
	log.Print(" Case 1:With all required data")
	// POST auth/accounts
	result, res := helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"type":  "USER",
		"email": "test@gmail.com",
		"nick":  "testnick",
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

// testWithEmptyData : test With Empty Data
func testWithEmptyData(t *testing.T) {
	log.Print(" Case 2:With empty data")
	// POST auth/accounts
	result, res := helpers.Register(
		t,
		"",
		"",
		"",
	)
	if res.StatusCode() != 412 {
		t.Errorf("Expected Response code: 412:Precondition failed but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Accounts must have an email address",
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

	log.Print(" Case 3:With password empty")
	// POST auth/accounts
	result, res = helpers.Register(
		t,
		"test@gmail.com",
		"",
		"test",
	)
	if res.StatusCode() != 412 {
		t.Errorf("Expected Response code: 412:Precondition failed but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"Error": "Accounts must have a password set",
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

	log.Print(" Case 4:With Nick empty")
	// POST auth/accounts
	result, res = helpers.Register(
		t,
		"test@gmail.com",
		"test",
		"",
	)
	if res.StatusCode() != 412 {
		t.Errorf("Expected Response code: 412:Precondition failed but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"Error": "Accounts must have a nick set",
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 4:Passed")
	} else {
		log.Print(" Case 4:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
}

// testEmailUniqueness : test Email Uniqueness
func testEmailUniqueness(t *testing.T) {
	log.Print(" Case 5:Email Uniqueness")
	// POST auth/accounts
	result, res := helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick2",
	)
	if res.StatusCode() != 412 {
		t.Errorf("Expected Response code 412:Precondition failed, but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Email or Nick already in use",
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 5:Passed")
	} else {
		log.Print(" Case 5:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
}

// testNickUniqueness : test Nick Uniqueness
func testNickUniqueness(t *testing.T) {
	log.Print(" Case 6:Nick Uniqueness")
	// POST auth/accounts
	result, res := helpers.Register(
		t,
		"test2@gmail.com",
		"testpassword",
		"testnick",
	)
	if res.StatusCode() != 412 {
		t.Errorf("Expected Response code 412:Precondition failed, but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Email or Nick already in use",
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 6:Passed")
	} else {
		log.Print(" Case 6:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
}
func setUpUserRegistration(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownUserRegistration(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
