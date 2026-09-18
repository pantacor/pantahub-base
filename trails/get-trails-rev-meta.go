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

type meta map[string]interface{}

// handleGetStep Get step meta just the raw data of a step without metainfo
// @Summary Get step meta just the raw data of a step without metainfo
// @Description Get step meta just the raw data of a step without metainfo
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Success 200 {object} meta
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/meta [get]
func (a *App) handleGetStepMeta(c *echo.Context) error {

	var err error

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType := ""
	authTypeData, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if ok {
		authType = authTypeData.(string)
	} else {
		return echoutil.RestErrorWrapper(c, "type of token is not defined", http.StatusForbidden)
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	step := trailmodels.Step{}
	trailID := c.Param("id")
	rev := c.Param("rev")

	isPublic, err := a.isTrailPublic(c.Request().Context(), trailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
	}

	if step.Meta == nil {
		step.Meta = map[string]interface{}{}
	}

	return echoutil.WriteJSON(c, http.StatusOK, utils.BsonUnquoteMap(&step.Meta))
}
