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
func (a *App) handlePutStepState(w rest.ResponseWriter, r *rest.Request) {

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

	step := Step{}
	trailID := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType != "USER" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER to put step state", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":             trailID + "-" + rev,
		"progress.status": "NEW",
		"garbage":         bson.M{"$ne": true},
	}).Decode(&step)

	if err != nil {
		utils.RestErrorWrapper(w, "Error with accessing data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if step.Owner != owner {
		utils.RestErrorWrapper(w, "No write access to step state", http.StatusForbidden)
	}

	stateMap := map[string]interface{}{}
	err = r.DecodeJsonPayload(&stateMap)
	if err != nil {
		utils.RestErrorWrapper(w, "Error with request: "+err.Error(), http.StatusBadRequest)
		return
	}

	step.StateSha, err = utils.StateSha(&stateMap)

	step.StepTime = time.Now()
	step.ProgressTime = time.Unix(0, 0)
	step.ID = trailID + "-" + rev

	objectList, err := ProcessObjectsInState(step.Owner, stateMap, a)
	if err != nil {
		utils.RestErrorWrapper(w, "Error processing step objects in state:"+err.Error(), http.StatusInternalServerError)
		return
	}
	step.UsedObjects = objectList
	step.State = utils.BsonQuoteMap(&stateMap)

	step.TimeModified = time.Now()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
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
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating step state: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		utils.RestErrorWrapper(w, "Error updating step state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	step.State = utils.BsonUnquoteMap(&step.State)
	w.WriteJson(step.State)
}
