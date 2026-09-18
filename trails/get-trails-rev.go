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
	"net/http"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/querymongo"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// handleGetStep Get step revision of the the given trail.
// @Summary Get step revision of the the given trail.
// @Description Get step revision of the the given trail.
// @Description Both user and device accounts can read the steps they own or who they are the
// @Description device of. devices can PUT progress to the /progress pseudo subnode. Besides
// @Description that steps are read only for the matter of the API
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Success 200 {object} trailmodels.Step
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev} [get]
func (a *App) handleGetStep(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	trailID := c.Param("id")
	isPublic, err := a.isTrailPublic(c.Request().Context(), trailID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	asp := querymongo.GetAllQueryPagination(c.Request().URL, filterByKeys)
	step := trailmodels.Step{}
	rev := c.Param("rev")
	query := bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	findOptions := options.FindOne()
	if asp.Fields != nil {
		findOptions.Projection = querymongo.MergeDefaultProjection(asp.Fields)
	}

	if isPublic {
		err = coll.FindOne(ctx, query, findOptions).Decode(&step)
	} else if authType == "DEVICE" {
		query["device"] = owner
		err = coll.FindOne(ctx, query, findOptions).Decode(&step)
	} else if authType == "USER" || authType == "SESSION" {
		query["owner"] = owner
		err = coll.FindOne(ctx, query, findOptions).Decode(&step)
	} else {
		return echoutil.RestErrorWrapper(c, "No Access to step", http.StatusForbidden)
	}

	if err != nil {
		// Missing revision is normal (devices poll for the next one): 404, not 500.
		// The query is owner/device-scoped, so this reveals nothing about other accounts.
		if errors.Is(err, mongo.ErrNoDocuments) {
			return echoutil.RestErrorWrapper(c, "No step "+rev+" for trail "+trailID, http.StatusNotFound)
		}
		return echoutil.RestErrorWrapper(c, "No access", http.StatusInternalServerError)
	}

	step.Meta = utils.BsonUnquoteMap(&step.Meta)
	step.State = utils.BsonUnquoteMap(&step.State)

	return echoutil.WriteJSON(c, http.StatusOK, step)
}
