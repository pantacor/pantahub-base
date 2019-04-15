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
	"gitlab.com/pantacor/pantahub-gc/models"
	"gitlab.com/pantacor/pantahub-testharness/helpers"
)

// TestMarkDeviceAsGarbage : Mark Devices as Garbage
func TestMarkDeviceAsGarbage(t *testing.T) {
	setUpMarkDeviceAsGarbage(t)
	log.Print("Test:Mark Devices as Garbage")
	// PUT markgarbage/device/<DEVICE_ID>
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	log.Print(" Case 1:Mark device as Garbage")
	result, res := helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 200 {
		t.Errorf("Error Marking Device As Garbage:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status":  1,
		"message": "Device marked as garbage",
		"device": map[string]interface{}{
			"id": device.ID.Hex(),
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
	tearDownMarkDeviceAsGarbage(t)
}

// TestMarkDeletedDeviceAsGarbage : Mark a deleted device as Garbage
func TestMarkDeletedDeviceAsGarbage(t *testing.T) {
	setUpMarkDeviceAsGarbage(t)
	log.Print(" Case 2:Mark a deleted device as Garbage")
	// Make the device invalid by deleting it
	device, res := helpers.CreateDevice(t, true, "123")
	if res.StatusCode() != 200 {
		t.Errorf("Error Creating Device:Expected Response code:200 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	helpers.DeleteDevice(t, device) //Error is handled isnide the function
	// Mark as garbage
	result, res := helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 400 {
		t.Errorf("Error Marking Invalid Device As Garbage:Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status": 0,
		"errors": map[string]interface{}{
			"id": []interface{}{"Document ID not found[ID:" + device.ID.Hex() + "]"},
		},
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
	tearDownMarkDeviceAsGarbage(t)
}
func TestMarkInvalidDeviceAsGarbage(t *testing.T) {
	setUpMarkDeviceAsGarbage(t)
	log.Print(" Case 3:Mark device with invalid Object ID as Garbage")
	device := models.Device{}
	device.ID = "123" //invalid Document Object ID
	// Mark as garbage
	result, res := helpers.MarkDeviceAsGarbage(t, device)
	if res.StatusCode() != 400 {
		t.Errorf("Error Marking Invalid Device As Garbage:Expected Response code:400 but got:" + strconv.Itoa(res.StatusCode()))
		t.Error(res)
	}
	expectedResult := map[string]interface{}{
		"status": 0,
		"errors": map[string]interface{}{
			"id": []interface{}{"Invalid Document ID[ID:" + device.ID.Hex() + "]"},
		},
	}
	if helpers.CheckResult(result, expectedResult) {
		log.Print(" Case 3:Passed")
	} else {
		log.Print(" Case 3:Failed")
		t.Errorf("Expected:")
		t.Error(expectedResult)
		t.Errorf("But Got:")
		t.Error(result)
		t.Fail()
	}
	tearDownMarkDeviceAsGarbage(t)
}
func setUpMarkDeviceAsGarbage(t *testing.T) bool {
	db.Connect()
	helpers.ClearOldData(t)
	//1.Login with user/user & Obtain Access token
	helpers.Login(t, "user1", "user1")
	return true
}
func tearDownMarkDeviceAsGarbage(t *testing.T) bool {
	helpers.ClearOldData(t)
	return true
}
