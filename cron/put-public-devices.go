//
// Copyright 2020  Pantacor Ltd.
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

package cron

import (
	"context"

	"net/http"

	"gitlab.com/pantacor/pantahub-base/callbacks"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// handlePutDevices Api to process steps of all public devices
// @Summary Api to process steps of a public device
// @Description Api to process steps of a public device
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {array} devices.Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /cron/devices [put]
func (a *App) handlePutDevices(w rest.ResponseWriter, r *rest.Request) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	callbackApp := callbacks.Build(a.mongoClient)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), a.CronJobTimeout)
	defer cancel()
	query := bson.M{
		"ispublic":              true,
		"mark_public_processed": bson.M{"$ne": true},
	}
	cur, err := collection.Find(ctx, query, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching public devices:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)

	response := []callbacks.ProcessDeviceResult{}

	for cur.Next(ctx) {
		device := devices.Device{}
		err := cur.Decode(&device)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		stepsMarkedAsNonPublic, stepsMarkedAsPublic, err := a.ProcessPublicDevice(&device)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Mark the flag "mark_public_processed" as TRUE
		err = callbackApp.MarkDeviceAsProcessed(device.ID)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
			return
		}
		data := callbacks.ProcessDeviceResult{
			DeviceID:               device.ID.Hex(),
			StepsMarkedAsPublic:    stepsMarkedAsPublic,
			StepsMarkedAsNonPublic: stepsMarkedAsNonPublic,
		}
		response = append(response, data)
	}
	w.WriteJson(response)
}

// ProcessPublicDevice is to process a public device
func (a *App) ProcessPublicDevice(device *devices.Device) (
	stepsMarkedAsNonPublic int,
	stepsMarkedAsPublic int,
	err error,
) {
	stepsMarkedAsNonPublic = 0
	stepsMarkedAsPublic = 0

	callbackApp := callbacks.Build(a.mongoClient)

	if device.IsPublic {
		// Mark all steps under the device as public
		stepsMarkedAsPublic, err = callbackApp.MarkDeviceStepsPublicFlag(device.ID, true)
	} else {
		// Mark all steps under the device as non-public
		stepsMarkedAsNonPublic, err = callbackApp.MarkDeviceStepsPublicFlag(device.ID, false)
	}

	return stepsMarkedAsNonPublic, stepsMarkedAsPublic, err
}
