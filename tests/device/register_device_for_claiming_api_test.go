//
// Copyright 2018  Pantacor Ltd.
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

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestRegisterDeviceForClaiming : Register Device Account For Claiming
func TestRegisterDeviceForClaiming(t *testing.T) {
	setUpRegisterDeviceForClaiming(t)

	log.Print("Test:Register Device For Claiming")

	log.Print(" Case 1:Register Device For Claiming")
	// POST auth/accounts
	result, res := helpers.CreateDevice(t, false, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := devices.Device{}
	expectedResult.Secret = "123"
	expectedResult.Owner = ""
	if expectedResult.Secret == result.Secret &&
		expectedResult.Owner == result.Owner {
		log.Print(" Case 1:Passed")
	} else {
		log.Print(" Case 1:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownRegisterDeviceForClaiming(t)
}
func setUpRegisterDeviceForClaiming(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownRegisterDeviceForClaiming(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
