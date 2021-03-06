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

// GetAllTrails : Test Get All Trails Of A Device
func TestGetAllTrails(t *testing.T) {
	connectToDb(t)
	setUpGetAllTrails(t)
	log.Print("Test:Get All Trails")
	t.Run("of a device", testGetAllTrailsOfDevice)
	tearDownGetAllTrails(t)
}

// testGetAllTrailsOfDevice : test Get All Trails Of A Device
func testGetAllTrailsOfDevice(t *testing.T) {
	log.Print(" Case 1:Get All Trails Of A Device")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha := helpers.GenerateObjectSha()
	_, _, res = helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.CreateTrail(t, device, true, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	result, res := helpers.LoginDevice(t, device.Prn, device.Secret)
	if res.StatusCode() != 200 {
		t.Errorf("Error Login Device Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
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
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownGetAllTrails(t *testing.T) bool {
	return true
}
