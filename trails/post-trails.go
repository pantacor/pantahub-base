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
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type state map[string]interface{}

// handlePostTrail Create a new trails
// @Summary Create a new trails
// @Description Create a new trails. usually done by device on first log in.
// @Description initiates the trail by using the reported state as stepwanted 0 and setting
// @Description the step 0 to be the POSTED JSON. Either device accounts or user accounts can
// @Description do this for devices owned, but there can always only be ONE trail per device.
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param body body state true "initial state"
// @Success 200 {object} trailmodels.Trail
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails [post]
func (a *App) handlePostTrail(c *echo.Context) error {
	rContext := context.WithoutCancel(c.Request().Context())
	initialState := map[string]interface{}{}

	if err := echoutil.DecodeJsonPayload(c, &initialState); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	device, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	if authType != "DEVICE" {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in as a DEVICE to post new trails", http.StatusForbidden)
	}

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["owner"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "Device needs an owner", http.StatusForbidden)
	}
	deviceID := prnGetID(device.(string))

	// do we need tip/tail here? or is that always read-only?
	newTrail := trailmodels.Trail{}
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	newTrail.ID = deviceObjectID
	newTrail.Owner = owner.(string)
	newTrail.Device = device.(string)
	newTrail.LastInSync = time.Time{}
	newTrail.LastTouched = newTrail.LastInSync

	autoLink := true
	autolinkValue, ok := c.Request().URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	ctx, cancel := context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	objectList, err := ProcessObjectsInState(ctx, newTrail.Owner, initialState, autoLink, a)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error processing trail objects in factory-state:"+err.Error(), http.StatusInternalServerError)
	}
	newTrail.UsedObjects = objectList
	newTrail.FactoryState = utils.BsonQuoteMap(&initialState)

	newStep := trailmodels.Step{}
	newStep.ID = newTrail.ID.Hex() + "-0"
	newStep.TrailID = newTrail.ID
	newStep.Rev = 0
	stateSha, err := utils.StateSha(&initialState)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error calculating state sha"+err.Error(), http.StatusInternalServerError)
	}
	newStep.StateSha = stateSha
	newStep.Owner = newTrail.Owner
	newStep.Device = newTrail.Device
	newStep.CommitMsg = "Factory State (rev 0)"

	now := time.Now()
	newStep.StepTime = now // XXX this should be factory time not now
	newStep.ProgressTime = now
	newStep.StepProgress.Status = "DONE"
	newStep.ProgressLog = []trailmodels.ProgressLogEntry{{
		Time:      now,
		Source:    trailmodels.ProgressLogSourceHub,
		Status:    newStep.StepProgress.Status,
		Progress:  100,
		StatusMsg: "factory state",
	}}
	newStep.Meta = map[string]interface{}{}
	newStep.TimeCreated = now
	newStep.TimeModified = now
	newStep.IsPublic = false

	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	isDevicePublic, err := a.IsDevicePublic(ctx, newStep.TrailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking device is public or not:"+err.Error(), http.StatusInternalServerError)
	}
	newStep.IsPublic = isDevicePublic

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	objectList, err = ProcessObjectsInState(ctx, newStep.Owner, initialState, autoLink, a)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error processing step objects in state: "+err.Error(), http.StatusInternalServerError)
	}
	newStep.UsedObjects = objectList
	newStep.State = utils.BsonQuoteMap(&initialState)

	// XXX: prototype: for production we need to prevent posting twice!!
	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newTrail,
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error inserting trail into database "+err.Error(), http.StatusInternalServerError)
	}

	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}
	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newStep,
	)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error inserting step into database "+err.Error(), http.StatusInternalServerError)
	}

	newTrail.FactoryState = utils.BsonUnquoteMap(&newTrail.FactoryState)
	return echoutil.WriteJSON(c, http.StatusOK, newTrail)
}
