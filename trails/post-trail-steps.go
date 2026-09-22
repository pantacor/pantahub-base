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
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"net/http"
	"strconv"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// handlePostStep Post a new step to the head of the trail.
// @Summary Post a new step to the head of the trail.
// @Description Post a new step to the head of the trail. You must include the correct Rev
// @Description number that must exactly be one incremented from the previous rev numbers.
// @Description In case of conflict creation of steps one will get an error.
// @Description In the DB the ID will be composite of trails ID + Rev; this ensures that
// @Description it will be unique. Also no step will be added if the previous one does not
// @Description exist that. This will include completeness of the step rev sequence.
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param body body trailmodels.Step true "Step Payload"
// @Success 200 {object} trailmodels.Trail
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps [post]
func (a *App) handlePostStep(c *echo.Context) error {
	rContext := context.WithoutCancel(c.Request().Context())
	var err error

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["owner"]

	// if not a device there won't be an owner; so we use the caller (aka prn)
	if !ok {
		owner, ok = c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
		if !ok {
			return echoutil.RestErrorWrapper(c, "You need to be logged in as user or device", http.StatusForbidden)
		}
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	trailID := c.Param("id")
	trail := trailmodels.Trail{}

	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}

	if authType == "USER" || authType == "DEVICE" || authType == "SESSION" {
		ctx, cancel := context.WithTimeout(rContext, 10*time.Second)
		defer cancel()

		query := bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		}
		err = collTrails.FindOne(ctx, query).Decode(&trail)
	} else {
		return echoutil.RestErrorWrapper(c, "Need to be logged in as USER to post trail steps", http.StatusForbidden)
	}

	if err != nil {
		return echoutil.RestErrorWrapper(c, "No resource access possible", http.StatusInternalServerError)
	}

	if trail.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No access", http.StatusForbidden)
	}

	// A device token may only post steps to its OWN trail. Without this a device
	// token (whose owner claim is the user's PRN) passes the owner check above for
	// every trail the user owns, letting one device deploy revisions to siblings.
	if authType == "DEVICE" {
		callerPrn, _ := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"].(string)
		if trail.Device != callerPrn {
			return echoutil.RestErrorWrapper(c, "No access", http.StatusForbidden)
		}
	}

	collDevices := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collDevices == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	var device devices.Device
	ctx, cancel := context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	err = collDevices.FindOne(ctx, bson.M{"_id": trail.ID}).Decode(&device)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "device doesn't exist", http.StatusInternalServerError)
	}

	newStep := trailmodels.Step{}
	if err := echoutil.DecodeJsonPayload(c, &newStep); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	autoLink := true
	autolinkValue, ok := c.Request().URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	created, status, err := a.CreateStep(rContext, trail, newStep, autoLink)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), status)
	}
	return echoutil.WriteJSON(c, http.StatusOK, created)
}

// ErrStepExists means the revision asked for was created in the meantime.
var ErrStepExists = errors.New("that revision already exists")

// CreateStep adds newStep to trail as the next revision the device is asked
// to run. newStep.Rev -1 means the one after the newest; any other value must
// follow an existing revision and not exist yet. The caller has checked that
// it may post to trail. It answers the HTTP status to report on failure.
func (a *App) CreateStep(rContext context.Context, trail trailmodels.Trail, newStep trailmodels.Step, autoLink bool) (*trailmodels.Step, int, error) {
	var (
		err    error
		ctx    context.Context
		cancel context.CancelFunc
	)
	previousStep := trailmodels.Step{}

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")
	collSteps := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if collTrails == nil || collSteps == nil {
		return nil, http.StatusInternalServerError, errors.New("Error with Database connectivity")
	}

	if newStep.Rev == -1 {
		ctx, cancel := context.WithTimeout(rContext, 10*time.Second)
		defer cancel()

		newStep.Rev, err = a.getLatestStepRev(ctx, trail.ID)
		if err != nil {
			return nil, http.StatusInternalServerError, errors.New("Error with getLatestStepRev: " + err.Error())
		}

		newStep.Rev++
	}

	if newStep.Rev > 0 {
		previousStepID := trail.ID.Hex() + "-" + strconv.Itoa(newStep.Rev-1)
		ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
		defer cancel()

		query := bson.M{
			"_id":     previousStepID,
			"garbage": bson.M{"$ne": true},
		}
		err = collSteps.FindOne(ctx, query).Decode(&previousStep)
		if err != nil {
			return nil, http.StatusInternalServerError, errors.New("No access to resource or bad step " + previousStepID)
		}
	}

	// XXX: introduce step diffs here and store them precalced

	newStep.ID = trail.ID.Hex() + "-" + strconv.Itoa(newStep.Rev)
	newStep.Owner = trail.Owner
	newStep.Device = trail.Device
	newStep.StepProgress = trailmodels.StepProgress{
		Status: "NEW",
	}
	newStep.TrailID = trail.ID
	now := time.Now()
	newStep.ProgressLog = []trailmodels.ProgressLogEntry{{
		Time:      now,
		Source:    trailmodels.ProgressLogSourceOwner,
		Status:    newStep.StepProgress.Status,
		StatusMsg: "step created",
	}}
	newStep.StepTime = now
	newStep.ProgressTime = time.Unix(0, 0)
	newStep.TimeCreated = now
	newStep.TimeModified = now

	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()

	isDevicePublic, err := a.IsDevicePublic(ctx, newStep.TrailID)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("Error checking device is public or not: " + err.Error())
	}
	newStep.IsPublic = isDevicePublic

	// IMPORTANT: statesha has to be before state as that will be escaped
	newStep.StateSha, err = utils.StateSha(&newStep.State)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("Error calculating Sha " + err.Error())
	}

	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()

	objectList, err := ProcessObjectsInState(ctx, newStep.Owner, newStep.State, autoLink, a)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("Error processing step objects in state: " + err.Error())
	}

	newStep.UsedObjects = objectList
	newStep.State = utils.BsonQuoteMap(&newStep.State)
	if newStep.Meta == nil {
		newStep.Meta = map[string]interface{}{}
	}
	newStep.Meta = utils.BsonQuoteMap(&newStep.Meta)
	newStep.TimeModified = time.Now()
	newStep.TimeCreated = time.Now()

	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	_, err = collSteps.InsertOne(
		ctx,
		newStep,
	)

	if mongo.IsDuplicateKeyError(err) {
		return nil, http.StatusConflict, fmt.Errorf("%w: revision %d", ErrStepExists, newStep.Rev)
	}
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("No access to resource or bad step rev1 " + err.Error())
	}
	ctx, cancel = context.WithTimeout(rContext, 10*time.Second)
	defer cancel()
	updateResult, err := collTrails.UpdateOne(
		ctx,
		bson.M{
			"_id":     trail.ID,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{
			"last-touched": newStep.StepTime,
		}},
	)
	if err != nil {
		log.Printf("Error updating last-touched for trail in poststep; not failing because step was written: %s\n  => ERROR: %s\n ", trail.ID.Hex(), err.Error())
	}
	if updateResult != nil && updateResult.MatchedCount == 0 {
		return nil, http.StatusBadRequest, errors.New("Trail not found")
	}

	newStep.State = utils.BsonUnquoteMap(&newStep.State)
	newStep.Meta = utils.BsonUnquoteMap(&newStep.Meta)

	return &newStep, http.StatusOK, nil
}
