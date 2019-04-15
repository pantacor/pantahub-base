//
// Copyright 2018-2019  Pantacor Ltd.
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

// TestMarkUnclaimedDevicesAsGarbage :Test Mark Unclaimed Devices As Garbage
func TestMarkUnclaimedDevicesAsGarbage(t *testing.T) {
	setUpMarkUnclaimedDevicesAsGarbage(t)
	log.Print("Test:Mark Unclaimed devices as garbages")
	// Case 1:Mark all unclaimed devices as garbage
	log.Print(" Case 1:Mark all unclaimed devices as garbage")
	device, res := helpers.CreateDevice(t, false, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	//3.Update device timecreated field to less than PANTAHUB_GC_UNCLAIMED_EXPIRY
	helpers.UpdateDeviceTimeCreated(t, &device) //Error is handled inside the function
	result, res := helpers.MarkAllUnClaimedDevicesAsGrabage(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking All UnClaimed Devices As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":         1,
		"devices_marked": helpers.DevicesCount,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 1:Passed")
	} else {
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownMarkUnclaimedDevicesAsGarbage(t)
}

// TestMarkUnclaimedDevicesAsGarbageWhenNoneLeftToMark : Mark all unclaimed devices as garbage when there is no unclaimed devices leftt to mark
func TestMarkUnclaimedDevicesAsGarbageWhenNoneLeftToMark(t *testing.T) {
	setUpMarkUnclaimedDevicesAsGarbage(t)
	// Case 2:Mark all unclaimed devices as garbage when there is no unclaimed devices leftt to mark
	log.Print(" Case 2:Mark all unclaimed devices as garbage when there is no unclaimed devices leftt to mark")
	helpers.MarkAllUnClaimedDevicesAsGrabage(t)
	result, res := helpers.MarkAllUnClaimedDevicesAsGrabage(t)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking All UnClaimed Devices As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":         1,
		"devices_marked": 0,
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 2:Passed")
	} else {
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownMarkUnclaimedDevicesAsGarbage(t)
}

func setUpMarkUnclaimedDevicesAsGarbage(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownMarkUnclaimedDevicesAsGarbage(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
