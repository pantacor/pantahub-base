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
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gopkg.in/mgo.v2/bson"
)

// handleGetTrailStepSummary Get steps summary of the the given trail.
// @Summary Get steps summary of the the given trail.
// @Description Get steps summary of the the given trail.
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
// @Success 200 {object} trailmodels.TrailSummary
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/summary [get]
func (a *App) handleGetTrailStepSummary(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	summaryCol := a.mongoClient.Database("pantabase_devicesummary").Collection("device_summary_short_new_v2")

	if summaryCol == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Need to be logged in as USER/SESSION user to get trail summary", http.StatusForbidden)
	}

	trailID := c.Param("id")

	if trailID == "" {
		return echoutil.RestErrorWrapper(c, "need to specify a device id", http.StatusForbidden)
	}

	query := bson.M{
		"deviceid": trailID,
		"garbage":  bson.M{"$ne": true},
		"$or": []bson.M{
			{"owner": owner},
			{"public": true},
		},
	}

	summary := trailmodels.TrailSummary{}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err := summaryCol.FindOne(ctx, query).Decode(&summary)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "error finding new trailId", http.StatusForbidden)
	}

	summary.FillLastSeen()

	if owner != summary.Owner {
		summary.FleetGroup = ""
		summary.FleetLocation = ""
		summary.FleetModel = ""
		summary.FleetRev = ""
		summary.RealIP = ""
	}
	return echoutil.WriteJSON(c, http.StatusOK, summary)
}
