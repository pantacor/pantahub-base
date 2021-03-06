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

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
	"go.mongodb.org/mongo-driver/mongo"
)

var MongoDb *mongo.Database

func connectToDb(t *testing.T) {
	MongoClient, err := utils.GetMongoClient()
	if err != nil {
		t.Errorf("Error Connecting to Db:" + err.Error())
	}
	MongoDb = MongoClient.Database(utils.MongoDb)
}

// TestCreateStep : Test Create Step
func TestCreateStep(t *testing.T) {
	connectToDb(t)
	setUpCreateStep(t)
	log.Print("Test:Create Step")
	t.Run("of a trail", testCreateStepOfATrail)
	tearDownCreateStep(t)
}

// testCreateStepOfATrail : test Create Step Of A Trail
func testCreateStepOfATrail(t *testing.T) {
	log.Print(" Case 1:Create Step Of A Trail")
	_, res := helpers.Login(t, "user1", "user1")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
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
		t.Errorf("Error Creating Trail:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	step, res := helpers.CreateStep(t, device, 1, true, sha)
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Step:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}

	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	result := map[string]interface{}{
		"id":         step.ID,
		"owner":      step.Owner,
		"device":     step.Device,
		"trail-id":   step.TrailID.Hex(),
		"rev":        step.Rev,
		"commit-msg": step.CommitMsg,
		"state":      step.State,
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
func setUpCreateStep(t *testing.T) bool {
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownCreateStep(t *testing.T) bool {
	return true
}
