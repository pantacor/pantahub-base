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

// TestListObjects : Test List Objects Of A User
func TestListObjects(t *testing.T) {
	connectToDb(t)
	setUpListObjects(t)
	log.Print("Test:List Objects")
	t.Run("of a user", testListObjectsOfUser)
	tearDownListObjects(t)
}

// testListLogsOfUser : test List Logs Of User
func testListObjectsOfUser(t *testing.T) {
	log.Print(" Case 1:List Objects Of User")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	sha := helpers.GenerateObjectSha()
	_, object1, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha = helpers.GenerateObjectSha()
	_, object2, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha = helpers.GenerateObjectSha()
	_, object3, res := helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	//log.Print(object1)
	//log.Print(object2)
	result, res := helpers.ListObjects(t)
	//log.Print(result)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := []interface{}{
		map[string]interface{}{
			"id":         object1.ID,
			"owner":      object1.Owner,
			"objectname": object1.ObjectName,
			"sha256sum":  object1.Sha,
		},
		map[string]interface{}{
			"id":         object2.ID,
			"owner":      object2.Owner,
			"objectname": object2.ObjectName,
			"sha256sum":  object2.Sha,
		},
		map[string]interface{}{
			"id":         object3.ID,
			"owner":      object3.Owner,
			"objectname": object3.ObjectName,
			"sha256sum":  object3.Sha,
		},
	}
	for k, v := range expectedResult {

		if helpers.CheckResult(
			result[k].(map[string]interface{}),
			v.(map[string]interface{}),
		) {
			log.Print(" Case 1[document:" + strconv.Itoa((k + 1)) + "]:Passed")
		} else {
			log.Print(" Case 1[document:" + strconv.Itoa((k + 1)) + "]:Failed")
			t.Errorf("Expected:")
			t.Error(v.(map[string]interface{}))
			t.Errorf("But Got:")
			t.Error(result[k].(map[string]interface{}))
			t.Fail()
		}
	}

}
func setUpListObjects(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownListObjects(t *testing.T) bool {
	return true
}
