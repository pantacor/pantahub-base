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
package helpers

import (
	"log"
	"strconv"
	"testing"

	"gitlab.com/pantacor/pantahub-gc/models"
)

// Devices : to keep the generated devices documents
var Devices []models.Device

// Trails : to keep the generated trails documents
var Trails []models.Trail

// Steps : to keep the generated steps documents
var Steps []models.Step

// Objects : to keep the generated object documents
var Objects []models.Object

// UTOKEN : to keep User access token
var UTOKEN = ""

// Counters to keep the expected values

// DevicesCount : to keep the generated DeviceCount
var DevicesCount = 0

// ObjectsCount : Objects Count
var ObjectsCount = 0

// StepsCount : Steps Count
var StepsCount = 0

// TrailsCount : TrailsCount
var TrailsCount = 0

// ReUsedObjectsCount : ReUsed Objects Count
var ReUsedObjectsCount = 0

// InvalidObjectsCount : Invalid Objects Count
var InvalidObjectsCount = 0

// InvalidTrailsCount : Invalid Trails Count
var InvalidTrailsCount = 0

// InvalidStepsCount : Invalid Steps Count
var InvalidStepsCount = 0

// ResetCounters : Reset Counters
func ResetCounters() {
	ReUsedObjectsCount = 0
	InvalidObjectsCount = 0
	InvalidTrailsCount = 0
	InvalidStepsCount = 0
	ObjectsCount = 0
	TrailsCount = 0
	StepsCount = 0
}

// DisplayCounters : Display Counters
func DisplayCounters() {
	log.Print("DevicesCount:" + strconv.Itoa(len(Devices)))
	log.Print("TrailsCount:" + strconv.Itoa(TrailsCount))
	log.Print("StepsCount:" + strconv.Itoa(StepsCount))
	log.Print("ObjectsCount:" + strconv.Itoa(ObjectsCount))
	log.Print("InvalidTrailsCount:" + strconv.Itoa(InvalidTrailsCount))
	log.Print("InvalidStepsCount:" + strconv.Itoa(InvalidStepsCount))
	log.Print("InvalidObjectsCount:" + strconv.Itoa(InvalidObjectsCount))
	log.Print("ReUsedObjectsCount:" + strconv.Itoa(ReUsedObjectsCount))
}

// ClearOldData : Clear all old data and reset counters
func ClearOldData(t *testing.T) bool {
	DeleteAllDevices(t)
	DeleteAllTrails(t)
	DeleteAllSteps(t)
	DeleteAllObjects(t)
	DeleteAllUserAccounts(t)
	ResetCounters()
	return true
}

// CheckResult : Used to compare expected result vs obtained result
func CheckResult(result map[string]interface{}, expectedResult map[string]interface{}) bool {
	for k, v := range expectedResult {
		switch v.(type) {
		case int:
			switch result[k].(type) {
			case int:
				if v.(int) != result[k].(int) {
					return false
				}
			case float64:
				if v.(int) != int(result[k].(float64)) {
					return false
				}
			}
		case string:
			//log.Print("Type:string")
			if v.(string) != result[k].(string) {
				return false
			}
		case bool:
			if v.(bool) != result[k].(bool) {
				return false
			}
		case map[string]interface{}:
			//log.Print("Type:map[string]interface{}")
			//recursion
			if !CheckResult(result[k].(map[string]interface{}), v.(map[string]interface{})) {
				return false
			}
		case []interface{}:
			//log.Print("Type:[]interface{}")
			for k2, v2 := range v.([]interface{}) {
				if v2.(string) != result[k].([]interface{})[k2].(string) {
					return false
				}
			}

		}

	}
	return true
}

// contains : to check if array contains an item or not
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
