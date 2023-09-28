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
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
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
func (a *App) handlePutStepProgress(w rest.ResponseWriter, r *rest.Request) {

	stepProgress := trailmodels.StepProgress{}
	r.DecodeJsonPayload(&stepProgress)
	trailID := r.PathParam("id")
	stepID := trailID + "-" + r.PathParam("rev")

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity - trails", http.StatusInternalServerError)
		return
	}

	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Only devices can update step status", http.StatusForbidden)
		return
	}

	progressTime := time.Now()

	deviceID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid device ID:"+err.Error(), http.StatusInternalServerError)
		return
	}

	isDevicePublic, err := a.IsDevicePublic(r.Context(), deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Error checking device is public or not:"+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":     stepID,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{
			"progress":      stepProgress,
			"progress-time": progressTime,
			"timemodified":  time.Now(),
			"ispublic":      isDevicePublic,
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating trail: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		utils.RestErrorWrapper(w, "Cannot update step progress "+err.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	updateResult, err = collTrails.UpdateOne(
		ctx,
		bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{"last-touched": progressTime}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating trail: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		// XXX: figure how to be better on error cases here...
		log.Printf("Error updating last-touched for trail in poststepprogress; not failing because step was written: %s\n", trailID)
	}

	w.WriteJson(stepProgress)
}
