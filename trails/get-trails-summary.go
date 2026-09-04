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
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// handleGetTrailSummary  get summary of all trails by the calling owner.
// @Summary  get summary of all trails by the calling owner.
// @Description  get summary of all trails by the calling owner.
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Success 200 {object} trailmodels.TrailSummary
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/summary [get]
func (a *App) handleGetTrailSummary(w rest.ResponseWriter, r *rest.Request) {

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

	sortParam := r.FormValue("sort")

	if sortParam == "" {
		sortParam = "-timestamp"
	}

	m := bson.M{}
	filterParam := r.FormValue("filter")
	if filterParam != "" {
		err := json.Unmarshal([]byte(filterParam), &m)
		if err != nil {
			utils.RestErrorWrapper(w, "Illegal Filter "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := mongoutils.ValidateClientFilter(map[string]interface{}(m)); err != nil {
			utils.RestErrorWrapper(w, "Illegal Filter: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// always filter by owner...
	m["owner"] = owner
	m["garbage"] = bson.M{"$ne": true}

	summaries := make([]trailmodels.TrailSummary, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	if sortParam[0:0] == "-" {
		sortParam = sortParam[1:] //removing "-"
		findOptions.SetSort(bson.M{sortParam: -1})
	} else {
		findOptions.SetSort(bson.M{sortParam: 1})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cur, err := summaryCol.Find(ctx, m, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching summaries:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := trailmodels.TrailSummary{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		result.FillLastSeen()
		summaries = append(summaries, result)
	}

	a.attachPendingOwnership(ctx, owner, summaries)

	w.WriteJson(summaries)
}

// attachPendingOwnership marks summaries of devices whose owner verification
// is still pending, so the device list can offer to accept them. The summary
// stream never carries ovmode, and only pending devices are looked up.
func (a *App) attachPendingOwnership(ctx context.Context, owner interface{}, summaries []trailmodels.TrailSummary) {
	if len(summaries) == 0 {
		return
	}
	devicesCol := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if devicesCol == nil {
		return
	}
	cur, err := devicesCol.Find(ctx, bson.M{
		"owner":         owner,
		"garbage":       bson.M{"$ne": true},
		"ovmode.mode":   bson.M{"$in": []string{models.TLSVerification.String(), models.ManualVerification.String()}},
		"ovmode.status": bson.M{"$nin": []string{models.Completed.String(), models.ValidationNotNeeded.String()}},
	}, options.Find().SetProjection(bson.M{"prn": 1, "ovmode": 1}))
	if err != nil {
		return
	}
	defer cur.Close(ctx)

	pending := map[string]*models.OVModeExtension{}
	for cur.Next(ctx) {
		doc := struct {
			Prn    string                  `bson:"prn"`
			OVMode *models.OVModeExtension `bson:"ovmode"`
		}{}
		if err := cur.Decode(&doc); err != nil || doc.OVMode == nil {
			continue
		}
		doc.OVMode.RootOfTrust = ""
		pending[doc.Prn] = doc.OVMode
	}
	if len(pending) == 0 {
		return
	}
	for i := range summaries {
		if ov, ok := pending[summaries[i].Device]; ok {
			summaries[i].OVMode = ov
		}
	}
}
