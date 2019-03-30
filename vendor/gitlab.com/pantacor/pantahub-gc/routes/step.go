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

// ProcessStepGarbages : Find all step documents with gc_processed=false then mark it associated objects as garbages
func ProcessStepGarbages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /processgarbages/steps Handler")
	}
	step := &models.Step{}
	_,
		stepsProcessed,
		objectsMarkedAsGarbage,
		stepsWithErrors,
		objectsWithErrors,
		objectsIgnored,
		warnings,
		errs := step.ProcessStepGarbages()

	if len(errs) > 0 {
		w.WriteHeader(http.StatusBadRequest)
		response := map[string]interface{}{
			"status":                    0,
			"errors":                    errs,
			"warnings":                  warnings,
			"steps_processed":           stepsProcessed,
			"objects_marked_as_garbage": objectsMarkedAsGarbage,
			"steps_with_errors":         stepsWithErrors,
			"objects_with_errors":       objectsWithErrors,
			"objects_ignored":           objectsIgnored,
		}

		json.NewEncoder(w).Encode(response)
	} else {
		response := map[string]interface{}{
			"status":                    1,
			"warnings":                  warnings,
			"steps_processed":           stepsProcessed,
			"objects_marked_as_garbage": objectsMarkedAsGarbage,
			"steps_with_errors":         stepsWithErrors,
			"objects_with_errors":       objectsWithErrors,
			"objects_ignored":           objectsIgnored,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

}

// PopulateStepsUsedObjects : Populate used_objects_field for all steps
func PopulateStepsUsedObjects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if utils.GetEnv("DEBUG") == "true" {
		log.Println("Inside PUT /populate/usedobjects/steps Handler")
	}
	step := &models.Step{}
	_,
		stepsPopulated,
		stepsWithErrors,
		errs := step.PopulateAllStepsUsedObjects()

	if len(errs) > 0 {

		response := map[string]interface{}{
			"errors":            errs,
			"status":            0,
			"steps_populated":   stepsPopulated,
			"steps_with_errors": stepsWithErrors,
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)

	} else {

		response := map[string]interface{}{
			"status":            1,
			"steps_populated":   stepsPopulated,
			"steps_with_errors": stepsWithErrors,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

}
