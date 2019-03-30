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
	"gitlab.com/pantacor/pantahub-gc/models"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestCreateTrail : Test Create Trail
func TestCreateTrail(t *testing.T) {
	setUpCreateTrail(t)
	log.Print("Test:Create Trail")
	t.Run("of a valid device", testCreateTrailOfValidDevice)
	t.Run("of an invalid device", testCreateTrailOfInvalidDevice)
	tearDownCreateTrail(t)
}

// testCreateTrailOfValidDevice : test Create Trail Of A Valid Device
func testCreateTrailOfValidDevice(t *testing.T) {
	log.Print(" Case 1:Create Trail Of A Valid Device")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	sha := helpers.GenerateObjectSha()
	_, _, res = helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	trail, res := helpers.CreateTrail(t, device, true, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	result := map[string]interface{}{
		"id":            trail.ID.Hex(),
		"owner":         trail.Owner,
		"device":        trail.Device,
		"factory-state": trail.FactoryState,
	}
	expectedResult := map[string]interface{}{
		"id":     device.ID.Hex(),
		"owner":  device.Owner,
		"device": device.Prn,
		"factory-state": map[string]interface{}{
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

// testCreateTrailOfInvalidDevice : test Create Trail Of An Invalid Device
func testCreateTrailOfInvalidDevice(t *testing.T) {
	log.Print(" Case 2:Create Trail Of An Invalid Device")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device := models.Device{
		ID: "5c4dcf7d80123b2f2c7e96e2",
	}
	sha := helpers.GenerateObjectSha()
	_, _, res = helpers.CreateObject(t, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Object:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	trail, res := helpers.CreateTrail(t, device, true, sha)
	if res.StatusCode() != 401 {
		t.Errorf("Expected Response code:401 UnAuthorized but got:" + strconv.Itoa(res.StatusCode()))
	}
	result := map[string]interface{}{
		"id":            trail.ID.Hex(),
		"owner":         trail.Owner,
		"device":        trail.Device,
		"factory-state": trail.FactoryState,
	}
	expectedResult := map[string]interface{}{
		"id":            "",
		"owner":         "",
		"device":        "",
		"factory-state": map[string]interface{}{},
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
func setUpCreateTrail(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownCreateTrail(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
