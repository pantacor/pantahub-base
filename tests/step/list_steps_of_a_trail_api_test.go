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

// TestListSteps : Test List Steps Of A Trail
func TestListSteps(t *testing.T) {
	setUpListSteps(t)

	log.Print("Test:List Steps Of A Trail")

	log.Print(" Case 1:List Steps Of A Trail")
	helpers.Login(t, "user1", "user1")
	//generate device & login to get DTOKEN
	device, _ := helpers.CreateDevice(t, true, "123")

	//generate object
	sha := helpers.GenerateObjectSha()
	helpers.CreateObject(t, sha)
	//create trail
	trail, _ := helpers.CreateTrail(t, device, true, sha)
	//add step rev=1
	helpers.CreateStep(t, device, 1, true, sha)
	//add step rev=2
	helpers.CreateStep(t, device, 2, true, sha)
	//add step rev=3
	helpers.CreateStep(t, device, 3, true, sha)

	result, res := helpers.ListSteps(t, trail.ID.Hex())
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}

	expectedResult := []interface{}{
		map[string]interface{}{
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
		},
		map[string]interface{}{
			"id":         trail.ID.Hex() + "-2",
			"owner":      trail.Owner,
			"device":     trail.Device,
			"trail-id":   trail.ID.Hex(),
			"rev":        2,
			"commit-msg": "Commit for Revision:2",
			"state": map[string]interface{}{
				"#spec":  "pantavisor-multi-platform@1",
				"kernel": sha,
			},
		},
		map[string]interface{}{
			"id":         trail.ID.Hex() + "-3",
			"owner":      trail.Owner,
			"device":     trail.Device,
			"trail-id":   trail.ID.Hex(),
			"rev":        3,
			"commit-msg": "Commit for Revision:3",
			"state": map[string]interface{}{
				"#spec":  "pantavisor-multi-platform@1",
				"kernel": sha,
			},
		},
	}
	for k, v := range result {

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

	tearDownListSteps(t)
}
func setUpListSteps(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownListSteps(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
