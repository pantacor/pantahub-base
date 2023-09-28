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
	"encoding/json"
	"net/http"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/querymongo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var filterByKeys = map[string]bool{}

// handleGetSteps Get steps of the the given trail.
// @Summary Get steps of the the given trail.
// @Description Get steps of the the given trail.
// @Description For user accounts querying this will return the list of steps that are not
// @Description DONE or in error state.
// @Description For device accounts querying this will return the list of unconfirmed steps.
// @Description Devices confirm a step by posting a walk element matching the rev.
// @Description This conveyes that the devices knows about the step to go and will keep the
// @Description post updates to the walk elements as they go.
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Success 200 {array} trailmodels.Step
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps [get]
func (a *App) handleGetSteps(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	asp := querymongo.GetAllQueryPagination(r.URL, filterByKeys)
	steps := make([]trailmodels.Step, 0)

	trailID := r.PathParam("id")
	query := bson.M{}

	isPublic, err := a.isTrailPublic(r.Context(), trailID)

	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
		return
	}
	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	if isPublic {
		query = bson.M{
			"trail-id":        trailObjectID,
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		}
	} else if authType == "DEVICE" {
		query = bson.M{
			"trail-id":        trailObjectID,
			"device":          owner,
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		}
	} else if authType == "USER" || authType == "SESSION" {
		query = bson.M{
			"trail-id":        trailObjectID,
			"owner":           owner,
			"progress.status": bson.M{"$ne": "DONE"},
			"garbage":         bson.M{"$ne": true},
		}
	}

	// allow override of progress.status defaults
	progressStatus := r.URL.Query().Get("progress.status")
	if progressStatus != "" {
		m := map[string]interface{}{}
		err := json.Unmarshal([]byte(progressStatus), &m)
		if err != nil {
			query["progress.status"] = progressStatus
		} else {
			query["progress.status"] = m
		}
	}

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	if authType == "DEVICE" && progressStatus == "" {
		findOptions.SetLimit(1)
	}

	sort := bson.M{"rev": 1}
	for key, value := range asp.Filters {
		query[key] = value
	}

	if asp.Fields != nil {
		findOptions.Projection = querymongo.MergeDefaultProjection(asp.Fields)
	}

	querymongo.SetMongoPagination(query, sort, asp.Pagination, findOptions)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cur, err := coll.Find(ctx, query, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching steps:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := trailmodels.Step{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusInternalServerError)
			return
		}
		result.Meta = utils.BsonUnquoteMap(&result.Meta)
		result.State = utils.BsonUnquoteMap(&result.State)
		steps = append(steps, result)
	}
	w.WriteJson(steps)
}
