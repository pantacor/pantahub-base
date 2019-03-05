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
//

package routes

import (
	"encoding/json"
	"log"
	"net/http"

	"gitlab.com/pantacor/pantahub-gc/models"

	"gitlab.com/pantacor/pantahub-base/utils"
)

// MarkDeviceAsGarbage : Mark a device as garbage
func MarkDeviceAsGarbage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /markgarbage/device/{id} Handler")
	}

	device := &models.Device{}

	_, errs := device.Validate(r)
	if len(errs) > 0 {

		w.WriteHeader(http.StatusBadRequest)
		response := map[string]interface{}{
			"status": 0,
			"errors": errs,
		}
		json.NewEncoder(w).Encode(response)

	} else {
		_, errs := device.MarkDeviceAsGrabage()

		if len(errs) > 0 {
			w.WriteHeader(http.StatusBadRequest)
			response := map[string]interface{}{
				"status": 0,
				"errors": errs,
			}
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"status":  1,
				"message": "Device marked as garbage",
				"device":  device,
			}
			json.NewEncoder(w).Encode(response)
		}

	}

}

// MarkUnClaimedDevicesAsGarbage : Mark all unclaimed devices as garbage after a while(eg: after 5 days)
func MarkUnClaimedDevicesAsGarbage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /markgarbage/devices/unclaimed Handler")
	}

	device := &models.Device{}
	_, devicesMarked, errs := device.MarkUnClaimedDevicesAsGrabage()

	if len(errs) > 0 {
		w.WriteHeader(http.StatusBadRequest)
		response := map[string]interface{}{
			"status": 0,
			"errors": errs,
		}
		json.NewEncoder(w).Encode(response)
	} else {
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"status":         1,
			"devices_marked": devicesMarked,
		}
		json.NewEncoder(w).Encode(response)
	}

}

// ProcessDeviceGarbages : Find all device documents with gc_processed=false then mark it associated trail as garbages
func ProcessDeviceGarbages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /processgarbages/device Handler")
	}
	device := &models.Device{}
	_,
		deviceProcessed,
		trailsMarkedAsGarbage,
		trailsWithErrors,
		errs := device.ProcessDeviceGarbages()

	if len(errs) > 0 {

		response := map[string]interface{}{
			"errors":                   errs,
			"status":                   0,
			"device_processed":         deviceProcessed,
			"trails_marked_as_garbage": trailsMarkedAsGarbage,
			"trails_with_errors":       trailsWithErrors,
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
	} else {
		response := map[string]interface{}{
			"status":                   1,
			"device_processed":         deviceProcessed,
			"trails_marked_as_garbage": trailsMarkedAsGarbage,
			"trails_with_errors":       trailsWithErrors,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

}

// DeleteDeviceGarbages : Delete Garbages of a Device
func DeleteDeviceGarbages(w http.ResponseWriter, r *http.Request) {

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside DeletekDeviceGarbages")
	}
	device := &models.Device{}
	result, response := device.DeleteGarbages()

	w.Header().Set("Content-Type", "application/json")
	if !result {
		if utils.GetEnv("PANTAHUB_GC_REMOVE_GARBAGE") != "true" {
			w.WriteHeader(http.StatusNotImplemented)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(response)
}
