//
// Copyright 2017,2018  Pantacor Ltd.
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

package dash

import (
	"context"
	"net/http"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

type accountClaims struct {
	Exp     string `json:"exp"`
	ID      string `json:"id"`
	Nick    string `json:"nick"`
	OrigIat string `json:"orig_iat"`
	Prn     string `json:"prn"`
	Roles   string `json:"roles"`
	Scopes  string `json:"scopes"`
	Type    string `json:"type"`
}

// handleAuth get account JWT token payload
// @Summary Get account JWT token payload
// @Description Get account JWT token claims payload
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags dash
// @Success 200 {object} accountClaims
// @Failure 400 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /dash/auth_status [get]
func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// handleGetSummary get account summary
// @Summary Get summary information about an Account
// @Description The summary contains information about the necessary data need
// to build a dashboard. This information includes the TopDevices used and information
// about your plan and how has been used your plan quota
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags dash
// @Success 200 {object} Summary
// @Failure 400 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /dash/ [get]
func (a *App) handleGetSummary(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	summaryCol := a.mongoClient.Database("pantabase_devicesummary").Collection("device_summary_short_new_v2")
	if summaryCol == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity (summaryCol)", http.StatusInternalServerError)
		return
	}

	dCol := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if dCol == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	oCol := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	if oCol == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	summary := Summary{}

	var mostRecentDeviceTrails []trails.TrailSummary
	findOptions := options.Find()
	findOptions.SetSort(bson.M{"timestamp": -1})
	findOptions.SetLimit(5)
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := summaryCol.Find(ctx, bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching devices:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := trails.TrailSummary{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		mostRecentDeviceTrails = append(mostRecentDeviceTrails, result)
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error finding devices for summary "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	summary.TopDevices = make([]DeviceInfo, 0)

	for _, v := range mostRecentDeviceTrails {
		dInfo := DeviceInfo{}
		dInfo.Prn = v.Device
		dInfo.Message = "Device changed at " + v.TrailTouchedTime.String()
		dInfo.Type = "INFO"
		dInfo.Nick = v.DeviceNick
		dInfo.DeviceID = v.DeviceID
		dInfo.Status = v.Status
		dInfo.LastActivity = v.Timestamp
		summary.TopDevices = append(summary.TopDevices, dInfo)
	}

	summary.Prn = owner.(string)
	summary.Nick = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["nick"].(string)

	sub, err := a.subService.LoadBySubject(utils.Prn(owner.(string)))
	if err != nil {
		sub = a.subService.GetDefaultSubscription(utils.Prn(owner.(string)))
	}

	plan := sub.GetPlan()
	prnInfo, err := plan.GetInfo()
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing plan "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	summary.Sub = SubscriptionInfo{
		PlanID:     prnInfo.Resource,
		Billing:    StandardPlans["AlphaTester"].Billing,
		QuotaStats: copySubToDashMap(sub),
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceCount, err := dCol.CountDocuments(ctx,
		bson.M{
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		},
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error finding devices for summary "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	quota := summary.Sub.QuotaStats[QuotaDevices]
	quota.Actual = float64(deviceCount)
	summary.Sub.QuotaStats[QuotaDevices] = quota

	// quota on disk
	resp := DiskQuotaUsageResult{}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipeline := []bson.M{
		bson.M{"$match": bson.M{
			"owner":   owner.(string),
			"garbage": bson.M{"$ne": true},
		}},
		bson.M{
			"$group": bson.M{
				"_id":   "$owner",
				"total": bson.M{"$sum": "$sizeint"},
			},
		},
	}
	//pipelineData, err := bson.Marshal(pipeline)
	if err != nil {
		utils.RestErrorWrapper(w, "ERROR Marshalling pipeline: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cur, err = oCol.Aggregate(ctx, pipeline)
	if err != nil {
		utils.RestErrorWrapper(w, "ERROR Aggregate pipeline data: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := DiskQuotaUsageResult{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR Decoding Document: "+err.Error(), http.StatusInternalServerError)
			return
		}
		resp = result
		break
	}

	if err == nil {
		quotaObjects := summary.Sub.QuotaStats[QuotaObjects]
		uM, err := units.ParseStrictBytes("1" + quotaObjects.Unit)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR Quota Unit: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fRound := float64(int64(float64(resp.Total)/float64(uM)*100)) / 100
		quotaObjects.Actual = fRound
		summary.Sub.QuotaStats[QuotaObjects] = quotaObjects
	} else if err != nil {
		utils.RestErrorWrapper(w, "Error finding quota usage of disk: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	w.WriteJson(summary)
}
