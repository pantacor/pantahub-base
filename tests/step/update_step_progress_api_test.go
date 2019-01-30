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

// TestUpdateStepProgress : Test Update Step Progress
func TestUpdateStepProgress(t *testing.T) {
	setUpUpdateStepProgress(t)
	log.Print("Test:Update Step Progress")
	t.Run("of a valid step", testUpdateStepProgress)
	tearDownUpdateStepProgress(t)
}

// testUpdateStepProgress : test Update Step Progress
func testUpdateStepProgress(t *testing.T) {
	log.Print(" Case 1:Update Step Progress")
	helpers.Login(t, "user1", "user1")
	device, _ := helpers.CreateDevice(t, true, "123")
	loginResult, res := helpers.LoginDevice(t, device.Prn, device.Secret)
	dToken := loginResult["token"].(string)

	sha := helpers.GenerateObjectSha()
	helpers.CreateObject(t, sha)
	trail, _ := helpers.CreateTrail(t, device, true, sha)
	helpers.CreateStep(t, device, 1, true, sha)
	progressData := map[string]interface{}{
		"log":        "log1",
		"progress":   50,
		"status":     "QUEUE",
		"status-msg": "test",
	}
	result, res := helpers.UpdateStepProgress(
		t,
		trail.ID.Hex(),
		"1",
		dToken,
		progressData,
	)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"log":        "log1",
		"progress":   50,
		"status":     "QUEUE",
		"status-msg": "test",
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
func setUpUpdateStepProgress(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownUpdateStepProgress(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
