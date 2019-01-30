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

// TestListLogs : Test ListLogs
func TestListLogs(t *testing.T) {
	setUpListLogs(t)
	log.Print("Test:List Logs")
	t.Run("of a user", testListLogsOfUser)
	tearDownListLogs(t)
}

// testListLogsOfUser : test List Logs Of User
func testListLogsOfUser(t *testing.T) {
	log.Print(" Case 1:List Logs Of User")
	helpers.Login(t, "user1", "user1")
	device, _ := helpers.CreateDevice(t, true, "123")
	loginResult, _ := helpers.LoginDevice(t, device.Prn, device.Secret)
	dToken := loginResult["token"].(string)

	logData1 := map[string]interface{}{
		"src":   "pantavisor1.log",
		"msg":   "My log line to remember1",
		"lvl":   "INFO1",
		"tsec":  1496532292,
		"tnano": 802110514,
	}
	logData2 := map[string]interface{}{
		"src":   "pantavisor2.log",
		"msg":   "My log line to remember2",
		"lvl":   "INFO2",
		"tsec":  1496532292,
		"tnano": 802110514,
	}
	logData3 := map[string]interface{}{
		"src":   "pantavisor3.log",
		"msg":   "My log line to remember3",
		"lvl":   "INFO3",
		"tsec":  1496532292,
		"tnano": 802110514,
	}
	helpers.CreateLog(t, dToken, logData1)
	helpers.CreateLog(t, dToken, logData2)
	helpers.CreateLog(t, dToken, logData3)
	result, res := helpers.ListLogs(t)
	//log.Print(result)
	if res.StatusCode() != 200 {
		t.Errorf("Expected Response code:200 OK but got:" + strconv.Itoa(res.StatusCode()))
	}
	expectedResult := map[string]interface{}{
		"entries": []interface{}{
			logData3,
			logData2,
			logData1,
		},
	}

	for k, v := range expectedResult["entries"].([]interface{}) {

		if helpers.CheckResult(
			result["entries"].([]interface{})[k].(map[string]interface{}),
			v.(map[string]interface{}),
		) {
			log.Print(" Case 1[document:" + strconv.Itoa((k + 1)) + "]:Passed")
		} else {
			log.Print(" Case 1[document:" + strconv.Itoa((k + 1)) + "]:Failed")
			t.Errorf("Expected:")
			t.Error(v.(map[string]interface{}))
			t.Errorf("But Got:")
			t.Error(result["entries"].([]interface{})[k].(map[string]interface{}))

			t.Fail()
		}
	}

}
func setUpListLogs(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	return true
}
func tearDownListLogs(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
