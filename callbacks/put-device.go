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

package callbacks

import (
	"context"
	"log"

	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ProcessDeviceResult api response
type ProcessDeviceResult struct {
	DeviceID               string `json:"device_id,omitempty"`
	StepsMarkedAsPublic    int    `json:"steps_marked_as_public"`
	StepsMarkedAsNonPublic int    `json:"steps_marked_as_non_public"`
}

// handlePutDevice Callback api for device changes
// @Summary Callback api for device changes
// @Description Callback api for device changes
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|Nick"
// @Success 200 {object} ProcessDeviceResult
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /callbacks/devices/{id} [put]
func (a *App) handlePutDevice(w rest.ResponseWriter, r *rest.Request) {
	var device devices.Device
	mgoid, err := primitive.ObjectIDFromHex(r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err = collection.FindOne(r.Context(),
		bson.M{
			"_id": mgoid,
		}).Decode(&device)
	if err == mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "Not Found", http.StatusNotFound)
		return
	} else if err != nil {
		log.Print(err.Error())
		utils.RestErrorWrapper(w, "Internal Error:"+err.Error(), http.StatusInternalServerError)
		return
	}
	timeModifiedStr, ok := r.URL.Query()["timemodified"]
	if ok {
		timeModified, err := time.Parse(time.RFC3339Nano, timeModifiedStr[0])
		if err != nil {
			utils.RestErrorWrapper(w, "Error Parsing timemodified:"+err.Error(), http.StatusForbidden)
			return
		}
		if device.TimeModified.After(timeModified) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	stepsMarkedAsNonPublic := 0
	stepsMarkedAsPublic := 0

	if device.IsPublic {
		// Mark all steps under the device as public
		stepsMarkedAsPublic, err = a.MarkDeviceStepsPublicFlag(r.Context(), device.ID, true)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		// Mark all steps under the device as non-public
		stepsMarkedAsNonPublic, err = a.MarkDeviceStepsPublicFlag(r.Context(), device.ID, false)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	// Mark the flag "mark_public_processed" as TRUE
	err = a.MarkDeviceAsProcessed(r.Context(), device.ID)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteJson(ProcessDeviceResult{
		DeviceID:               device.ID.Hex(),
		StepsMarkedAsPublic:    stepsMarkedAsPublic,
		StepsMarkedAsNonPublic: stepsMarkedAsNonPublic,
	})
}

// MarkDeviceAsProcessed is used to mark a device as processed
func (a *App) MarkDeviceAsProcessed(ctx context.Context, ID primitive.ObjectID) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := collection.UpdateOne(
		ctxC,
		bson.M{"_id": ID},
		bson.M{"$set": bson.M{
			"mark_public_processed": true,
		}},
		nil,
	)
	if err != nil {
		return err
	}

	return nil
}

// MarkDeviceStepsPublicFlag mark all device steps public flag by device ID
func (a *App) MarkDeviceStepsPublicFlag(ctx context.Context, ID primitive.ObjectID, public bool) (int, error) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	updateResult, err := collection.UpdateMany(
		ctxC,
		bson.M{
			"trail-id": ID,
			"ispublic": bson.M{
				"$ne": public,
			},
		},
		bson.M{
			"$set": bson.M{
				"ispublic":     public,
				"timemodified": time.Now(),
			},
		},
		nil,
	)
	if err != nil {
		return int(updateResult.MatchedCount), err
	}

	return int(updateResult.MatchedCount), nil
}
