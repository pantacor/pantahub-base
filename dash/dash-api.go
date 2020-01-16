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
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

// App define a new rest application for dash
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
	subService    subscriptions.SubscriptionService
}

// QuotaType type of quota
type QuotaType string

// QuotaValue quota value
type QuotaValue string

// PlanQuotas map with thefinitions of all plans
type PlanQuotas map[QuotaType]QuotaValue

// BillingInfo billing information, how amount to be charged, current and vat
type BillingInfo struct {
	Type      string
	AmountDue float32
	Currency  string
	VatRegion string
}

// Plan definition of a billing plan
type Plan struct {
	Name    string
	Quotas  map[QuotaType]Quota
	Billing BillingInfo
}

// Quota definition of a quota
type Quota struct {
	Name   QuotaType
	Actual float64
	Max    float64
	Unit   string
}

// SubscriptionInfo subscription information
type SubscriptionInfo struct {
	PlanID     string              `json:"plan-id"`
	Billing    BillingInfo         `json:"billing"`
	QuotaStats map[QuotaType]Quota `json:"quota-stats"`
}

// DeviceInfo define the payload for device information
type DeviceInfo struct {
	DeviceID     string    `json:"device-id"`
	Nick         string    `json:"nick"`
	Prn          string    `json:"prn"`
	Message      string    `json:"message"`
	Type         string    `json:"type"`
	Status       string    `json:"status"`
	LastActivity time.Time `json:"last-activity"`
}

// Summary user dashboard summary including their top devices and subscription
type Summary struct {
	Prn        string           `json:"prn"`
	Nick       string           `json:"nick"`
	Sub        SubscriptionInfo `json:"subscription"`
	TopDevices []DeviceInfo     `json:"top-devices"`
}

// DiskQuotaUsageResult define disk usage metrics
type DiskQuotaUsageResult struct {
	ID    string  `json:"id" bson:"_id"`
	Total float64 `json:"total"`
}

const (
	// QuotaObjects type of quota used by objects
	QuotaObjects = QuotaType("OBJECTS")

	// QuotaBandwidth type of quota used by bandwith metrics
	QuotaBandwidth = QuotaType("BANDWIDTH")

	// QuotaDevices type of quota used by devices
	QuotaDevices = QuotaType("DEVICES")

	// QuotaBillingDays type of quota used for billing period
	QuotaBillingDays = QuotaType("BILLINGPERIOD")
)

var (
	// StandardBilling default billing info for standard plans
	StandardBilling = BillingInfo{
		Type:      "Monthly",
		AmountDue: 0,
		Currency:  "USD",
		VatRegion: "World",
	}

	// StandardPlans define standard plans
	StandardPlans = map[string]Plan{
		"AlphaTester": Plan{
			Name: "AlphaTester",
			Quotas: map[QuotaType]Quota{
				QuotaObjects: Quota{
					Name: QuotaObjects,
					Max:  2,
					Unit: "GiB",
				},
				QuotaBandwidth: Quota{
					Name: QuotaBandwidth,
					Max:  2,
					Unit: "GiB",
				},
				QuotaDevices: Quota{
					Name: QuotaDevices,
					Max:  25,
					Unit: "Piece",
				},
				QuotaBillingDays: Quota{
					Name: QuotaBillingDays,
					Max:  30,
					Unit: "Days",
				},
			},
			Billing: BillingInfo{
				Type:      "Monthly",
				AmountDue: 0,
				Currency:  "USD",
				VatRegion: "World",
			},
		},
		"VIP": Plan{
			Name: "VIP",
			Quotas: map[QuotaType]Quota{
				QuotaObjects: Quota{
					Name: QuotaObjects,
					Max:  25,
					Unit: "GiB",
				},
				QuotaBandwidth: Quota{
					Name: QuotaBandwidth,
					Max:  50,
					Unit: "GiB",
				},
				QuotaDevices: Quota{
					Name: QuotaDevices,
					Max:  100,
					Unit: "Piece",
				},
				QuotaBillingDays: Quota{
					Name: QuotaBillingDays,
					Max:  30,
					Unit: "Days",
				},
			},
			Billing: BillingInfo{
				Type:      "Monthly",
				AmountDue: 0,
				Currency:  "USD",
				VatRegion: "World",
			},
		},
	}
)

// ModelError error payload (code, message)
type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func copySubToDashMap(sub subscriptions.Subscription) map[QuotaType]Quota {
	newMap := map[QuotaType]Quota{}

	deviceQuota := sub.GetProperty(string(QuotaDevices))
	deviceQuotaI, err := strconv.ParseFloat(deviceQuota.(string), 64)

	if err != nil {
		log.Printf("WARNING: subscription (%s) with illegal deviceQuota value: %s\n",
			sub.GetPrn(), deviceQuota)
		deviceQuotaI = 0
	}
	newMap[QuotaDevices] = Quota{
		Name: QuotaDevices,
		Max:  float64(deviceQuotaI),
		Unit: "Piece",
	}
	objectsQuota := sub.GetProperty(string(QuotaObjects))
	objectsQuotaI, err := units.ParseStrictBytes(objectsQuota.(string))
	if err != nil {
		objectsQuotaI = 0
	}
	objectsQuotaG := units.Base2Bytes(objectsQuotaI) / units.Gibibyte
	newMap[QuotaObjects] = Quota{
		Name: QuotaObjects,
		Max:  float64(objectsQuotaG),
		Unit: "GiB",
	}
	networkQuota := sub.GetProperty(string(QuotaBandwidth))
	networkQuotaI, err := units.ParseStrictBytes(networkQuota.(string))
	if err != nil {
		objectsQuotaI = 0
	}
	networkQuotaG := units.Base2Bytes(networkQuotaI) / units.GiB
	newMap[QuotaBandwidth] = Quota{
		Name: QuotaBandwidth,
		Max:  float64(networkQuotaG),
		Unit: "GiB",
	}

	return newMap
}

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

// New create a dash rest application
func New(jwtMiddleware *jwt.JWTMiddleware,
	subService subscriptions.SubscriptionService,
	mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient
	app.subService = subService

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/dash:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "dash"})

	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// all need auth
			return true
		},
		IfTrue: app.jwtMiddleware,
	})

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// all need auth
			return true
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	// /auth_status endpoints
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/auth_status", handleAuth),
		rest.Get("/", app.handleGetSummary),
	)
	app.API.SetApp(apiRouter)

	return app
}
