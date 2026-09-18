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
	"net/http"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gopkg.in/mgo.v2/bson"
)

// handlePutStepState Put step state (only if not yet consumed)
// @Summary Put step state (only if not yet consumed)
// @Description put step state (only if not yet consumed). just the raw data of a step without metainfo like pvr pu
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Param body body state true "payload"
// @Success 200 {object} state
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/state [post]
func (a *App) handlePutStepState(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	step := trailmodels.Step{}
	trailID := c.Param("id")
	rev := c.Param("rev")

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Need to be logged in as USER/SESSION user to put step state", http.StatusForbidden)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":             trailID + "-" + rev,
		"progress.status": "NEW",
		"garbage":         bson.M{"$ne": true},
	}).Decode(&step)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with accessing data: "+err.Error(), http.StatusInternalServerError)
	}

	if step.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No write access to step state", http.StatusForbidden)
	}

	stateMap := map[string]interface{}{}
	err = echoutil.DecodeJsonPayload(c, &stateMap)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with request: "+err.Error(), http.StatusBadRequest)
	}

	step.StateSha, err = utils.StateSha(&stateMap)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with request: "+err.Error(), http.StatusBadRequest)
	}

	step.StepTime = time.Now()
	step.ProgressTime = time.Unix(0, 0)
	step.ID = trailID + "-" + rev
	step.ProgressLog = trailmodels.AppendProgressLog(step.ProgressLog, trailmodels.ProgressLogEntry{
		Time:      step.StepTime,
		Source:    trailmodels.ProgressLogSourceOwner,
		Status:    step.StepProgress.Status,
		Progress:  step.StepProgress.Progress,
		StatusMsg: "state replaced",
	})

	autoLink := true
	autolinkValue, ok := c.Request().URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	objectList, err := ProcessObjectsInState(ctx, step.Owner, stateMap, autoLink, a)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error processing step objects in state: "+err.Error(), http.StatusInternalServerError)
	}
	step.UsedObjects = objectList
	step.State = utils.BsonQuoteMap(&stateMap)

	step.TimeModified = time.Now()

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	isDevicePublic, err := a.IsDevicePublic(ctx, step.TrailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking device is public or not: "+err.Error(), http.StatusInternalServerError)
	}
	step.IsPublic = isDevicePublic

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":             trailID + "-" + rev,
			"owner":           owner,
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		},
		bson.M{"$set": step},
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating step state: "+err.Error(), http.StatusInternalServerError)
	}

	if updateResult.MatchedCount == 0 {
		return echoutil.RestErrorWrapper(c, "Error updating step state: not found", http.StatusBadRequest)
	}

	step.State = utils.BsonUnquoteMap(&step.State)
	return echoutil.WriteJSON(c, http.StatusOK, step.State)
}
