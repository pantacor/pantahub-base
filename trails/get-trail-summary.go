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
	"net/http"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handleGetTrailStepSummary(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	summaryCol := a.mongoClient.Database("pantabase_devicesummary").Collection("device_summary_short_new_v2")

	if summaryCol == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER/SESSION user to get trail summary", http.StatusForbidden)
		return
	}

	trailID := r.PathParam("id")

	if trailID == "" {
		utils.RestErrorWrapper(w, "need to specify a device id", http.StatusForbidden)
		return
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err := summaryCol.FindOne(ctx, query).Decode(&summary)

	if err != nil {
		utils.RestErrorWrapper(w, "error finding new trailId", http.StatusForbidden)
		return
	}

	if owner != summary.Owner {
		summary.FleetGroup = ""
		summary.FleetLocation = ""
		summary.FleetModel = ""
		summary.FleetRev = ""
		summary.RealIP = ""
	}
	w.WriteJson(summary)
}
