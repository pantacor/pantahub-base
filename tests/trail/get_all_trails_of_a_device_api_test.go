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

// GetAllTrails : Test Get All Trails Of A Device
func TestGetAllTrails(t *testing.T) {
	setUpGetAllTrails(t)
	log.Print("Test:Get All Trails")
	t.Run("of a device", testGetAllTrailsOfDevice)
	tearDownGetAllTrails(t)
}

// testGetAllTrailsOfDevice : test Get All Trails Of A Device
func testGetAllTrailsOfDevice(t *testing.T) {
	log.Print(" Case 1:Get All Trails Of A Device")
	helpers.Login(t, "user1", "user1")
	sha := helpers.GenerateObjectSha()
	helpers.CreateObject(t, sha)
	device, _ := helpers.CreateDevice(t, true, "123")
	helpers.CreateTrail(t, device, true, sha)

	result, _ := helpers.LoginDevice(t, device.Prn, device.Secret)
	dToken := result["token"].(string)
	trailsResult, res := helpers.ListTrails(t, device.ID.Hex(), dToken)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := []interface{}{
		map[string]interface{}{
			"id":     device.ID.Hex(),
			"owner":  device.Owner,
			"device": device.Prn,
			"factory-state": map[string]interface{}{
				"#spec":  "pantavisor-multi-platform@1",
				"kernel": sha,
			},
		},
	}
	for k, v := range trailsResult {

		if helpers.CheckResult(
			v.(map[string]interface{}),
			expectedResult[k].(map[string]interface{}),
		) {
			log.Print(" Case 1[document:" + strconv.Itoa((k + 1)) + "]:Passed")
		} else {
			log.Print(" Case 1[document:" + strconv.Itoa((k + 1)) + "]:Failed")
			t.Errorf("Expected:")
			t.Error(expectedResult[k].(map[string]interface{}))
			t.Errorf("But Got:")
			t.Error(v.(map[string]interface{}))
			t.Fail()
		}
	}
}
func setUpGetAllTrails(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownGetAllTrails(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
