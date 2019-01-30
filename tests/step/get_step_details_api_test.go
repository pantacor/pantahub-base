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

// TestGetStepDetails : Test Get Step Details
func TestGetStepDetails(t *testing.T) {
	setUpCreateStep(t)
	log.Print("Test:Get Step Details")
	t.Run("of a valid step", testStepDetails)
	tearDownCreateStep(t)
}

// testStepDetails : test Step Details
func testStepDetails(t *testing.T) {
	log.Print(" Case 1:Step Details")
	helpers.Login(t, "user1", "user1")
	device, _ := helpers.CreateDevice(t, true, "123")
	sha := helpers.GenerateObjectSha()
	helpers.CreateObject(t, sha)
	trail, _ := helpers.CreateTrail(t, device, true, sha)
	helpers.CreateStep(t, device, 1, true, sha)

	result, res := helpers.GetStep(t, trail.ID.Hex(), "1")
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"id":         trail.ID.Hex() + "-1",
		"owner":      trail.Owner,
		"device":     trail.Device,
		"trail-id":   trail.ID.Hex(),
		"rev":        1,
		"commit-msg": "Commit for Revision:1",
		"state": map[string]interface{}{
			"#spec":  "pantavisor-multi-platform@1",
			"kernel": sha,
		},
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
func setUpStepDetails(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownStepDetails(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
