//
// Copyright (c) 2017-2026 Pantacor Ltd.
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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"log"
	"net/http"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// handlePutStepProgress Post Step Progress information for a step.
// @Summary Post Step Progress information for a step.
// @Description Post Step Progress information for a step.
// @Description Only device accounts can put status info. they are expected to provide at status field.
// @Description all input paramaters besides the device-progress one are ignored.
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Param body body trailmodels.StepProgress true "StepProgress payload"
// @Success 200 {object} trailmodels.StepProgress
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/progress [put]
func (a *App) handlePutStepProgress(c *echo.Context) error {

	stepProgress := trailmodels.StepProgress{}
	if err := echoutil.DecodeJsonPayload(c, &stepProgress); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}
	trailmodels.SanitizeStepProgress(&stepProgress)
	trailID := c.Param("id")
	stepID := trailID + "-" + c.Param("rev")

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity - trails", http.StatusInternalServerError)
	}

	if authType != "DEVICE" {
		return echoutil.RestErrorWrapper(c, "Only devices can update step status", http.StatusForbidden)
	}

	progressTime := time.Now()

	deviceID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid device ID:"+err.Error(), http.StatusInternalServerError)
	}

	isDevicePublic, err := a.IsDevicePublic(c.Request().Context(), deviceID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking device is public or not:"+err.Error(), http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":     stepID,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		},
		// pipeline update: stores the report and appends a progress-log line
		// (capped, deduplicated) in the same atomic write
		trailmodels.ProgressUpdatePipeline(stepProgress, trailmodels.ProgressLogSourceDevice, progressTime, bson.M{
			"progress-time": progressTime,
			"timemodified":  time.Now(),
			"ispublic":      isDevicePublic,
		}),
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Cannot update step progress "+err.Error(), http.StatusForbidden)
	}

	if updateResult.MatchedCount == 0 {
		return echoutil.RestErrorWrapper(c, "Error updating trail: not found", http.StatusBadRequest)
	}
	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	updateResult, err = collTrails.UpdateOne(
		ctx,
		bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{"last-touched": progressTime}},
	)
	if err != nil {
		// XXX: figure how to be better on error cases here...
		log.Printf("Error updating last-touched for trail in poststepprogress; not failing because step was written: %s\n", trailID)
	}

	if updateResult.MatchedCount == 0 {
		return echoutil.RestErrorWrapper(c, "Error updating trail: not found", http.StatusBadRequest)
	}

	return echoutil.WriteJSON(c, http.StatusOK, stepProgress)
}
