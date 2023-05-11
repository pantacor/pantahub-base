// Copyright (c) 2017-2023 Pantacor Ltd.
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

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestVerifyUserAccount : Test Verify User Account
func TestVerifyUserAccount(t *testing.T) {
	connectToDb(t)
	setUpVerifyUserAccount(t)
	log.Print("Test:Verify User Account")
	t.Run("With valid account", testValidAccount)
	t.Run("With invalid account", testInvalidAccount)
	tearDownVerifyUserAccount(t)
}

// testValidAccount : test Valid Account
func testValidAccount(t *testing.T) {
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
	account := helpers.GetUser(t, "test@gmail.com", MongoDb)
	result, res := helpers.VerifyUserAccount(t, account.Id.Hex(), account.Challenge)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"type":  "USER",
		"email": "test@gmail.com",
		"nick":  "testnick",
		"prn":   "prn:::accounts:/" + account.Id.Hex(),
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
	//Trying to verify again
	log.Print(" Case 2:Verying account which is already verified")
	result, res = helpers.VerifyUserAccount(t, account.Id.Hex(), account.Challenge)
	if res.StatusCode() != 412 {
		t.Errorf("Expected Response code:412 Precondition failed, but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult = map[string]interface{}{
		"Error": "Invalid Challenge (wrong, used or never existed)",
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

// testInvalidAccount : test Invalid Account
func testInvalidAccount(t *testing.T) {
	log.Print(" Case 3:Invalid Account")
	// POST auth/accounts

	account := accounts.Account{}
	//Setting Invalid Challenge string and object id
	account.Challenge = "InvalidChallenge"
	ObjectID, _ := primitive.ObjectIDFromHex("5c4da57680123b2d60b28060")
	account.ID = ObjectID

	result, res := helpers.VerifyUserAccount(t, account.Id.Hex(), account.Challenge)

	if res.StatusCode() != 403 {
		t.Errorf("Expected Response code 403:Forbidden failed but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"Error": "Not Accessible Resource Id",
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
func setUpVerifyUserAccount(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownVerifyUserAccount(t *testing.T) bool {
	return true
}
