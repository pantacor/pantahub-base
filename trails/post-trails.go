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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"net/http"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
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
// @Success 200 {object} Trail
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails [post]
func (a *App) handlePostTrail(w rest.ResponseWriter, r *rest.Request) {

	initialState := map[string]interface{}{}

	r.DecodeJsonPayload(&initialState)

	device, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "DEVICE" {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a DEVICE to post new trails", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Device needs an owner", http.StatusForbidden)
		return
	}
	deviceID := prnGetID(device.(string))

	// do we need tip/tail here? or is that always read-only?
	newTrail := Trail{}
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newTrail.ID = deviceObjectID
	newTrail.Owner = owner.(string)
	newTrail.Device = device.(string)
	newTrail.LastInSync = time.Time{}
	newTrail.LastTouched = newTrail.LastInSync

	autoLink := true
	autolinkValue, ok := r.URL.Query()["autolink"]
	if ok && autolinkValue[0] == "no" {
		autoLink = false
	}

	objectList, err := ProcessObjectsInState(r.Context(), newTrail.Owner, initialState, autoLink, a)
	if err != nil {
		utils.RestErrorWrapper(w, "Error processing trail objects in factory-state:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newTrail.UsedObjects = objectList
	newTrail.FactoryState = utils.BsonQuoteMap(&initialState)

	newStep := Step{}
	newStep.ID = newTrail.ID.Hex() + "-0"
	newStep.TrailID = newTrail.ID
	newStep.Rev = 0
	stateSha, err := utils.StateSha(&initialState)
	if err != nil {
		utils.RestErrorWrapper(w, "Error calculating state sha"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.StateSha = stateSha
	newStep.Owner = newTrail.Owner
	newStep.Device = newTrail.Device
	newStep.CommitMsg = "Factory State (rev 0)"

	now := time.Now()
	newStep.StepTime = now // XXX this should be factory time not now
	newStep.ProgressTime = now
	newStep.StepProgress.Status = "DONE"
	newStep.Meta = map[string]interface{}{}
	newStep.TimeCreated = now
	newStep.TimeModified = now
	newStep.IsPublic = false

	isDevicePublic, err := a.IsDevicePublic(r.Context(), newStep.TrailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Error checking device is public or not:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.IsPublic = isDevicePublic

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	objectList, err = ProcessObjectsInState(r.Context(), newStep.Owner, initialState, autoLink, a)
	if err != nil {
		utils.RestErrorWrapper(w, "Error processing step objects in state"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.UsedObjects = objectList
	newStep.State = utils.BsonQuoteMap(&initialState)

	// XXX: prototype: for production we need to prevent posting twice!!
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newTrail,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error inserting trail into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newStep,
	)

	if err != nil {
		utils.RestErrorWrapper(w, "Error inserting step into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	newTrail.FactoryState = utils.BsonUnquoteMap(&newTrail.FactoryState)
	w.WriteJson(newTrail)
}
