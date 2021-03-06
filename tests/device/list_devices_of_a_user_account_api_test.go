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

// TestListDevicesOfUserAccount : Test List Devices Of User Account
func TestListDevicesOfUserAccount(t *testing.T) {
	connectToDb(t)
	setUpListDevicesOfUserAccount(t)

	log.Print("Test:List Devices Of A User Account")

	log.Print(" Case 1:List Devices Of A User Account")
	// POST auth/accounts
	_, res := helpers.Register(
		t,
		"test@gmail.com",
		"testpassword",
		"testnick",
	)
	if res.StatusCode() != 200 {
		t.Errorf("Error Register User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	account := helpers.GetUser(t, "test@gmail.com", MongoDb)
	_, res = helpers.VerifyUserAccount(t, account.Id.Hex(), account.Challenge)
	if res.StatusCode() != 200 {
		t.Errorf("Error Verifying User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	_, res = helpers.Login(t, "testnick", "testpassword")
	if res.StatusCode() != 200 {
		t.Errorf("Error Login User Account:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device1, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device1:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device2, res := helpers.CreateDevice(t, true, "345")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device2:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	device3, res := helpers.CreateDevice(t, true, "678")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device3:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
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
	helpers.ClearOldData(t, MongoDb)
	return true
}
func tearDownListDevicesOfUserAccount(t *testing.T) bool {
	return true
}
