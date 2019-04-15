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

	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestRefreshToken : Test Refresh Token
func TestRefreshToken(t *testing.T) {
	connectToDb(t)
	setUpRefreshToken(t)
	log.Print("Test:Refresh Token")
	t.Run("With valid token", testValidToken)
	t.Run("With invalid token", testInvalidToken)
	tearDownRefreshToken(t)
}

// testValidToken : test Valid Token
func testValidToken(t *testing.T) {
	log.Print(" Case 1:Valid token")
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
	_, res = helpers.VerifyUserAccount(t, account.Id.Hex(), account.Challenge)
	if res.StatusCode() != 200 {
		t.Errorf("Error Verifying User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, _ := helpers.Login(t, "testnick", "testpassword")
	result, res = helpers.RefreshToken(t, result["token"].(string))

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

// testInvalidToken : test Invalid Token
func testInvalidToken(t *testing.T) {
	log.Print(" Case 2:Invalid token")
	invalidToken := "invalidtoken"
	result, res := helpers.RefreshToken(t, invalidToken)
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

}
func setUpRefreshToken(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownRefreshToken(t *testing.T) bool {
	return true
}
