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

// TestListDevicesOfUserAccount : Test List Devices Of User Account
func TestListDevicesOfUserAccount(t *testing.T) {
	setUpListDevicesOfUserAccount(t)

	log.Print("Test:List Devices Of A User Account")

	log.Print(" Case 1:List Devices Of A User Account")
	// POST auth/accounts
	helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	account := helpers.GetUser(t, "test@gmail.com")
	helpers.VerifyUserAccount(t, account)
	helpers.Login(t, "testnick", "testpassword")
	device1, _ := helpers.CreateDevice(t, true, "123")
	device2, _ := helpers.CreateDevice(t, true, "345")
	device3, _ := helpers.CreateDevice(t, true, "678")
	result, res := helpers.ListUserDevices(t)

	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}

	expectedResult := []interface{}{
		map[string]interface{}{
			"id":     device1.ID.Hex(),
			"prn":    device1.Prn,
			"nick":   device1.Nick,
			"owner":  device1.Owner,
			"secret": device1.Secret,
		},
		map[string]interface{}{
			"id":     device2.ID.Hex(),
			"prn":    device2.Prn,
			"nick":   device2.Nick,
			"owner":  device2.Owner,
			"secret": device2.Secret,
		},
		map[string]interface{}{
			"id":     device3.ID.Hex(),
			"prn":    device3.Prn,
			"nick":   device3.Nick,
			"owner":  device3.Owner,
			"secret": device3.Secret,
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

	tearDownListDevicesOfUserAccount(t)
}
func setUpListDevicesOfUserAccount(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownListDevicesOfUserAccount(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
