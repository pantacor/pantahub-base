//
// Copyright (c) 2017-2023 Pantacor Ltd.
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
	"strconv"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
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
func (a *App) handlePostStep(w rest.ResponseWriter, r *rest.Request) {
	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]

	// if not a device there wont be an owner; so we use the caller (aka prn)
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
		if !ok {
			// XXX: find right error
			utils.RestErrorWrapper(w, "You need to be logged in as user or device", http.StatusForbidden)
			return
		}
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	trailID := r.PathParam("id")
	trail := trailmodels.Trail{}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	if authType == "USER" || authType == "DEVICE" || authType == "SESSION" {
		err = collTrails.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	} else {
		utils.RestErrorWrapper(w, "Need to be logged in as USER to post trail steps", http.StatusForbidden)
		return
	}

	if err != nil {
		utils.RestErrorWrapper(w, "No resource access possible", http.StatusInternalServerError)
		return
	}

	if trail.Owner != owner {
		utils.RestErrorWrapper(w, "No access", http.StatusForbidden)
		return
	}

	collSteps := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collSteps == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	newStep := trailmodels.Step{}
	previousStep := trailmodels.Step{}
	r.DecodeJsonPayload(&newStep)

	if newStep.Rev == -1 {
		trailObjectID, err := primitive.ObjectIDFromHex(trailID)
		if err != nil {
			utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		newStep.Rev, err = a.getLatestStepRev(ctx, trailObjectID)
		if err != nil {
			utils.RestErrorWrapper(w, "Error with getLatestStepRev: "+err.Error(), http.StatusInternalServerError)
			return
		}
		newStep.Rev++
	}

	if err != nil {
		utils.RestErrorWrapper(w, "Error auto appending step 1 "+err.Error(), http.StatusInternalServerError)
		return
	}

	stepID := trailID + "-" + strconv.Itoa(newStep.Rev-1)
	ctx, cancel = context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err = collSteps.FindOne(ctx, bson.M{
		"_id":     stepID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&previousStep)

	if err != nil {
		// XXX: figure how to be better on error cases here...
		utils.RestErrorWrapper(w, "No access to resource or bad step "+stepID, http.StatusInternalServerError)
		return
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
	newStep.StepTime = now
	newStep.ProgressTime = time.Unix(0, 0)
	newStep.TimeCreated = now
	newStep.TimeModified = now
	newStep.IsPublic = previousStep.IsPublic

	isDevicePublic, err := a.IsDevicePublic(r.Context(), newStep.TrailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Error checking device is public or not:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.IsPublic = isDevicePublic

	// IMPORTANT: statesha has to be before state as that will be escaped
	newStep.StateSha, err = utils.StateSha(&newStep.State)

	if err != nil {
		utils.RestErrorWrapper(w, "Error calculating Sha "+err.Error(), http.StatusInternalServerError)
		return
	}

	autoLink := true
	autolinkValue, ok := r.URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	objectList, err := ProcessObjectsInState(r.Context(), newStep.Owner, newStep.State, autoLink, a)
	if err != nil {
		utils.RestErrorWrapper(w, "Error processing step objects in state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.UsedObjects = objectList
	newStep.State = utils.BsonQuoteMap(&newStep.State)
	if newStep.Meta == nil {
		newStep.Meta = map[string]interface{}{}
	}
	newStep.Meta = utils.BsonQuoteMap(&newStep.Meta)
	newStep.TimeModified = time.Now()
	newStep.TimeCreated = time.Now()

	ctx, cancel = context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = collSteps.InsertOne(
		ctx,
		newStep,
	)

	if err != nil {
		// XXX: figure how to be better on error cases here...
		utils.RestErrorWrapper(w, "No access to resource or bad step rev1 "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 5*time.Second)
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
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Trail not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		// XXX: figure how to be better on error cases here...
		log.Printf("Error updating last-touched for trail in poststep; not failing because step was written: %s\n  => ERROR: %s\n ", trail.ID.Hex(), err.Error())
	}

	newStep.State = utils.BsonUnquoteMap(&newStep.State)
	newStep.Meta = utils.BsonUnquoteMap(&newStep.Meta)

	w.WriteJson(newStep)
}
