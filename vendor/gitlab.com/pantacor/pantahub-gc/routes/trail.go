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

// MarkAllTrailGarbages : Mark trails as garbage that lost their parent device
func MarkAllTrailGarbages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /processgarbages/trail Handler")
	}
	_,
		trailsMarked,
		errs := models.MarkAllTrailGarbages()

	if len(errs) > 0 {
		response := map[string]interface{}{
			"errors":        errs,
			"status":        0,
			"trails_marked": trailsMarked,
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
	} else {
		response := map[string]interface{}{
			"status":        1,
			"trails_marked": trailsMarked,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

}

// ProcessTrailGarbages : Find all trail documents with gc_processed=false then mark it associated steps as garbages
func ProcessTrailGarbages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /processgarbages/trail Handler")
	}
	trail := &models.Trail{}
	_,
		trailsProcessed,
		stepsMarkedAsGarbage,
		objectsMarkedAsGarbage,
		trailsWithErrors,
		objectsWithErrors,
		objectsIgnored,
		stepsWithErrors,
		warnings,
		errs := trail.ProcessTrailGarbages()

	if len(errs) > 0 {

		response := map[string]interface{}{
			"status":                    0,
			"errors":                    errs,
			"warnings":                  warnings,
			"trails_processed":          trailsProcessed,
			"steps_marked_as_garbage":   stepsMarkedAsGarbage,
			"objects_marked_as_garbage": objectsMarkedAsGarbage,
			"trails_with_errors":        trailsWithErrors,
			"objects_with_errors":       objectsWithErrors,
			"objects_ignored":           objectsIgnored,
			"steps_with_errors":         stepsWithErrors,
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
	} else {
		response := map[string]interface{}{
			"status":                    1,
			"warnings":                  warnings,
			"trails_processed":          trailsProcessed,
			"steps_marked_as_garbage":   stepsMarkedAsGarbage,
			"objects_marked_as_garbage": objectsMarkedAsGarbage,
			"trails_with_errors":        trailsWithErrors,
			"objects_with_errors":       objectsWithErrors,
			"objects_ignored":           objectsIgnored,
			"steps_with_errors":         stepsWithErrors,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

}

// PopulateTrailsUsedObjects : Populate used_objects_field for all trails
func PopulateTrailsUsedObjects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /populate/usedobjects/trails Handler")
	}
	trail := &models.Trail{}
	_,
		trailsPopulated,
		trailsWithErrors,
		errs := trail.PopulateAllTrailsUsedObjects()

	if len(errs) > 0 {

		response := map[string]interface{}{
			"errors":             errs,
			"status":             0,
			"trails_populated":   trailsPopulated,
			"trails_with_errors": trailsWithErrors,
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)

	} else {

		response := map[string]interface{}{
			"status":             1,
			"trails_populated":   trailsPopulated,
			"trails_with_errors": trailsWithErrors,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

}
