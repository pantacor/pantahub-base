//
// Copyright 2017  Pantacor Ltd.
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
	"log"
	"net/http"
	"os"
	"strconv"

	jwt "github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type DashApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
	subService     subscriptions.SubscriptionService
}

type QuotaType string
type QuotaValue string
type PlanQuotas map[QuotaType]QuotaValue

type BillingInfo struct {
	Type      string
	AmountDue float32
	Currency  string
	VatRegion string
}

type Plan struct {
	Name    string
	Quotas  map[QuotaType]Quota
	Billing BillingInfo
}

type Quota struct {
	Name   QuotaType
	Actual float64
	Max    float64
	Unit   string
}

type SubscriptionInfo struct {
	PlanId     string              `json:"plan-id"`
	Billing    BillingInfo         `json:"billing"`
	QuotaStats map[QuotaType]Quota `json:"quota-stats"`
}

type DeviceInfo struct {
	DeviceId string `json:"device-id"`
	Nick     string `json:"nick"`
	Prn      string `json:"prn"`
	Message  string `json:"message"`
	Type     string `json:"type"`
	Status   string `json:"status"`
}

type Summary struct {
	Prn        string           `json:"prn"`
	Nick       string           `json:"nick"`
	Sub        SubscriptionInfo `json:"subscription"`
	TopDevices []DeviceInfo     `json:"top-devices"`
}

type DiskQuotaUsageResult struct {
	Id    string  `json:"id" bson:"_id"`
	Total float64 `json:"total"`
}

const (
	QUOTA_OBJECTS     = QuotaType("OBJECTS")
	QUOTA_BANDWIDTH   = QuotaType("BANDWIDTH")
	QUOTA_DEVICES     = QuotaType("DEVICES")
	QUOTA_BILLINGDAYS = QuotaType("BILLINGPERIOD")
)

var (
	StandardBilling = BillingInfo{
		Type:      "Monthly",
		AmountDue: 0,
		Currency:  "USD",
		VatRegion: "World",
	}
	STANDARD_PLANS = map[string]Plan{
		"AlphaTester": Plan{
			Name: "AlphaTester",
			Quotas: map[QuotaType]Quota{
				QUOTA_OBJECTS: Quota{
					Name: QUOTA_OBJECTS,
					Max:  2,
					Unit: "GiB",
				},
				QUOTA_BANDWIDTH: Quota{
					Name: QUOTA_BANDWIDTH,
					Max:  2,
					Unit: "GiB",
				},
				QUOTA_DEVICES: Quota{
					Name: QUOTA_DEVICES,
					Max:  25,
					Unit: "Piece",
				},
				QUOTA_BILLINGDAYS: Quota{
					Name: QUOTA_BILLINGDAYS,
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
				QUOTA_OBJECTS: Quota{
					Name: QUOTA_OBJECTS,
					Max:  25,
					Unit: "GiB",
				},
				QUOTA_BANDWIDTH: Quota{
					Name: QUOTA_BANDWIDTH,
					Max:  50,
					Unit: "GiB",
				},
				QUOTA_DEVICES: Quota{
					Name: QUOTA_DEVICES,
					Max:  100,
					Unit: "Piece",
				},
				QUOTA_BILLINGDAYS: Quota{
					Name: QUOTA_BILLINGDAYS,
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

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func copySubToDashMap(sub subscriptions.Subscription) map[QuotaType]Quota {
	newMap := map[QuotaType]Quota{}

	deviceQuota := sub.GetProperty(string(QUOTA_DEVICES))
	deviceQuotaI, err := strconv.ParseFloat(deviceQuota.(string), 64)

	if err != nil {
		log.Printf("WARNING: subscription (%s) with illegal deviceQuota value: %s\n",
			sub.GetPrn(), deviceQuota)
		deviceQuotaI = 0
	}
	newMap[QUOTA_DEVICES] = Quota{
		Name: QUOTA_DEVICES,
		Max:  float64(deviceQuotaI),
		Unit: "Piece",
	}
	objectsQuota := sub.GetProperty(string(QUOTA_OBJECTS))
	objectsQuotaI, err := units.ParseStrictBytes(objectsQuota.(string))
	if err != nil {
		objectsQuotaI = 0
	}
	objectsQuotaG := units.Base2Bytes(objectsQuotaI) / units.Gibibyte
	newMap[QUOTA_OBJECTS] = Quota{
		Name: QUOTA_OBJECTS,
		Max:  float64(objectsQuotaG),
		Unit: "GiB",
	}
	networkQuota := sub.GetProperty(string(QUOTA_BANDWIDTH))
	networkQuotaI, err := units.ParseStrictBytes(networkQuota.(string))
	if err != nil {
		objectsQuotaI = 0
	}
	networkQuotaG := units.Base2Bytes(networkQuotaI) / units.GiB
	newMap[QUOTA_BANDWIDTH] = Quota{
		Name: QUOTA_BANDWIDTH,
		Max:  float64(networkQuotaG),
		Unit: "GiB",
	}

	return newMap
}

func (a *DashApp) handle_getsummary(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	summaryCol := a.mgoSession.DB("pantabase_devicesummary").C("device_summary_short_new_v1")
	if summaryCol == nil {
		rest.Error(w, "Error with Database connectivity (summaryCol)", http.StatusInternalServerError)
		return
	}

	dCol := a.mgoSession.DB("").C("pantahub_devices")
	if dCol == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	oCol := a.mgoSession.DB("").C("pantahub_objects")
	if oCol == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	summary := Summary{}

	var mostRecentDeviceTrails []trails.TrailSummary
	err := summaryCol.Find(bson.M{"owner": owner}).Sort("-timestamp").Limit(5).All(&mostRecentDeviceTrails)

	if err != nil {
		rest.Error(w, "Error finding devices for summary "+err.Error(),
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
		dInfo.DeviceId = v.DeviceId
		dInfo.Status = v.Status
		summary.TopDevices = append(summary.TopDevices, dInfo)
	}

	summary.Prn = owner.(string)
	summary.Nick = r.Env["JWT_PAYLOAD"].(map[string]interface{})["nick"].(string)

	sub, err := a.subService.LoadBySubject(utils.Prn(owner.(string)))
	if err != nil && err != mgo.ErrNotFound {
		rest.Error(w, "Error finding subscription for summary "+err.Error(),
			http.StatusInternalServerError)
		return
	}
	if err == mgo.ErrNotFound {
		sub = a.subService.GetDefaultSubscription(utils.Prn(owner.(string)))
	}

	plan := sub.GetPlan()
	prnInfo, err := plan.GetInfo()
	if err != nil {
		rest.Error(w, "Error parsing plan "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	summary.Sub = SubscriptionInfo{
		PlanId:     prnInfo.Resource,
		Billing:    STANDARD_PLANS["AlphaTester"].Billing,
		QuotaStats: copySubToDashMap(sub),
	}

	deviceCount, err :=
		dCol.Find(bson.M{"owner": owner}).Count()

	if err != nil {
		rest.Error(w, "Error finding devices for summary "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	quota := summary.Sub.QuotaStats[QUOTA_DEVICES]
	quota.Actual = float64(deviceCount)
	summary.Sub.QuotaStats[QUOTA_DEVICES] = quota

	// quota on disk
	resp := DiskQuotaUsageResult{}
	err = oCol.Pipe([]bson.M{{"$match": bson.M{"owner": owner.(string)}},
		{"$group": bson.M{"_id": "$owner", "total": bson.M{"$sum": "$sizeint"}}}}).One(&resp)

	if err == nil {
		quotaObjects := summary.Sub.QuotaStats[QUOTA_OBJECTS]
		uM, err := units.ParseStrictBytes("1" + quotaObjects.Unit)
		if err != nil {
			rest.Error(w, "ERROR Quota Unit: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fRound := float64(int64(float64(resp.Total)/float64(uM)*100)) / 100
		quotaObjects.Actual = fRound
		summary.Sub.QuotaStats[QUOTA_OBJECTS] = quotaObjects
	} else if err != nil && err != mgo.ErrNotFound {
		rest.Error(w, "Error finding quota usage of disk: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	w.WriteJson(summary)
}

func New(jwtMiddleware *jwt.JWTMiddleware,
	subService subscriptions.SubscriptionService,
	session *mgo.Session) *DashApp {

	app := new(DashApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session
	app.subService = subService

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/dash:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "dash"})

	app.Api.Use(rest.DefaultCommonStack...)
	app.Api.Use(&rest.CorsMiddleware{
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

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// all need auth
			return true
		},
		IfTrue: app.jwt_middleware,
	})

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", handle_auth),
		rest.Get("/", app.handle_getsummary),
	)
	app.Api.SetApp(api_router)

	return app
}
