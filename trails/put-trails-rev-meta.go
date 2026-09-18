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

// handlePutStepMeta Put step meta just the raw data of a step without metainfo like pvr put
// @Summary Put step meta just the raw data of a step without metainfo like pvr put
// @Description Put step meta just the raw data of a step without metainfo like pvr put
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Param body body meta true "payload"
// @Success 200 {object} meta
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/meta [put]
func (a *App) handlePutStepMeta(c *echo.Context) error {

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

	step := trailmodels.Step{}
	trailID := c.Param("id")
	rev := c.Param("rev")

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Need to be logged in as USER/SESSION user to put step meta", http.StatusForbidden)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).Decode(&step)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with accessing data: "+err.Error(), http.StatusInternalServerError)
	}

	if step.Owner != owner {
		return echoutil.RestErrorWrapper(c, "No write access to step meta", http.StatusForbidden)
	}

	metaMap := map[string]interface{}{}
	err = echoutil.DecodeJsonPayload(c, &metaMap)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error with request: "+err.Error(), http.StatusBadRequest)
	}

	step.Meta = utils.BsonQuoteMap(&metaMap)

	step.TimeModified = time.Now()

	isDevicePublic, err := a.IsDevicePublic(c.Request().Context(), step.TrailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking device is public or not:"+err.Error(), http.StatusInternalServerError)
	}
	step.IsPublic = isDevicePublic

	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":     trailID + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": step},
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating step meta: "+err.Error(), http.StatusInternalServerError)
	}

	if updateResult.MatchedCount == 0 {
		return echoutil.RestErrorWrapper(c, "Error updating step meta: not found", http.StatusBadRequest)
	}

	step.Meta = utils.BsonUnquoteMap(&step.Meta)
	return echoutil.WriteJSON(c, http.StatusOK, step.Meta)
}
