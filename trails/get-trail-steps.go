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
	"encoding/json"
	"net/http"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
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
func (a *App) handleGetSteps(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	asp := querymongo.GetAllQueryPagination(c.Request().URL, filterByKeys)
	steps := make([]trailmodels.Step, 0)

	trailID := c.Param("id")
	query := bson.M{}

	isPublic, err := a.isTrailPublic(c.Request().Context(), trailID)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
	}
	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
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
	progressStatus := c.Request().URL.Query().Get("progress.status")
	if progressStatus != "" {
		m := map[string]interface{}{}
		err := json.Unmarshal([]byte(progressStatus), &m)
		if err != nil {
			query["progress.status"] = progressStatus
		} else {
			if err := mongoutils.ValidateClientFilter(m); err != nil {
				return echoutil.RestErrorWrapper(c, "Illegal progress.status filter: "+err.Error(), http.StatusBadRequest)
			}
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

	if len(asp.Fields) > 0 {
		findOptions.Projection = querymongo.MergeDefaultProjection(asp.Fields)
	} else {
		// the progress log is per-step detail; keep list payloads flat unless
		// the caller asks for it explicitly through ?fields=
		findOptions.Projection = bson.M{trailmodels.ProgressLogField: 0}
	}

	querymongo.SetMongoPagination(query, sort, asp.Pagination, findOptions)

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	cur, err := coll.Find(ctx, query, findOptions)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error on fetching steps:"+err.Error(), http.StatusForbidden)
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := trailmodels.Step{}
		err := cur.Decode(&result)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusInternalServerError)
		}
		result.Meta = utils.BsonUnquoteMap(&result.Meta)
		result.State = utils.BsonUnquoteMap(&result.State)
		steps = append(steps, result)
	}
	return echoutil.WriteJSON(c, http.StatusOK, steps)
}
