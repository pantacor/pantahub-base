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

// TestCreateLog : Test Create Log Of A Device
func TestCreateLog(t *testing.T) {
	setUpCreateLog(t)
	log.Print("Test:Create Log Of A Device")
	t.Run("of a trail", testCreateLog)
	tearDownCreateLog(t)
}

// testCreateLog : test Create Log Of A Device
func testCreateLog(t *testing.T) {
	log.Print(" Case 1:Create Log Of A Device")
	helpers.Login(t, "user1", "user1")
	device, _ := helpers.CreateDevice(t, true, "123")
	loginResult, _ := helpers.LoginDevice(t, device.Prn, device.Secret)
	dToken := loginResult["token"].(string)

	logData := map[string]interface{}{
		"src":   "pantavisor.log",
		"msg":   "My log line to remember",
		"lvl":   "INFO",
		"tsec":  1496532292,
		"tnano": 802110514,
	}
	result, res := helpers.CreateLog(t, dToken, logData)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := []interface{}{
		map[string]interface{}{
			"dev":   device.Prn,
			"own":   device.Owner,
			"tsec":  1496532292,
			"tnano": 802110514,
			"src":   "pantavisor.log",
			"lvl":   "INFO",
			"msg":   "My log line to remember",
		},
	}
	if helpers.CheckResult(
		result[0].(map[string]interface{}),
		expectedResult[0].(map[string]interface{}),
	) {
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
func setUpCreateLog(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownCreateLog(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
