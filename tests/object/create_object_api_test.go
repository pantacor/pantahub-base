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

// TestCreateObject : Test Create Object
func TestCreateObject(t *testing.T) {
	setUpCreateObject(t)
	log.Print("Test:Create Object")
	t.Run("of a trail", testCreateObject)
	tearDownCreateObject(t)
}

// testCreateObject : test Create Object
func testCreateObject(t *testing.T) {
	log.Print(" Case 1:Create Object")
	helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	account := helpers.GetUser(t, "test@gmail.com")
	helpers.VerifyUserAccount(t, account)
	helpers.Login(t, "testnick", "testpassword")
	//helpers.Login(t, "user1", "user1")
	sha := helpers.GenerateObjectSha()
	_, object, res := helpers.CreateObject(t, sha)

	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	result := map[string]interface{}{
		"id":         object.ID,
		"owner":      object.Owner,
		"objectname": object.ObjectName,
		"sha256sum":  object.Sha,
	}
	expectedResult := map[string]interface{}{
		"id":         sha,
		"owner":      account.Prn,
		"objectname": "",
		"sha256sum":  sha,
	}
	if helpers.CheckResult(
		result,
		expectedResult,
	) {
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
func setUpCreateObject(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownCreateObject(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
