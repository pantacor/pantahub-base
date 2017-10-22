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

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/trails"
)

type DashApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
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
	Actual int64
	Max    int64
	Unit   string
}

type SubscriptionInfo struct {
	PlanId     string              `json:"plan-id"`
	Billing    BillingInfo         `json:"billing"`
	QuotaStats map[QuotaType]Quota `json:"quota-stats"`
}

type DeviceInfo struct {
	DeviceId bson.ObjectId `json:"device-id"`
	Nick     string        `json:"nick"`
	Prn      string        `json:"prn"`
	Message  string        `json:"message"`
	Type     string        `json:"type"`
}

type Summary struct {
	Prn        string           `json:"prn"`
	Nick       string           `json:"nick"`
	Sub        SubscriptionInfo `json:"subscription"`
	TopDevices []DeviceInfo     `json:"top-devices"`
}

const (
	QUOTA_OBJECTS     = QuotaType("OBJECTS")
	QUOTA_BANDWIDTH   = QuotaType("BANDWIDTH")
	QUOTA_DEVICES     = QuotaType("DEVICES")
	QUOTA_BILLINGDAYS = QuotaType("BILLINGPERIOD")
)

var (
	STANDARD_PLANS = map[string]Plan{
		"AlphaTester": Plan{
			Name: "AlphaTester",
			Quotas: map[QuotaType]Quota{
				QUOTA_OBJECTS: Quota{
					Name: QUOTA_OBJECTS,
					Max:  5,
					Unit: "GiB",
				},
				QUOTA_BANDWIDTH: Quota{
					Name: QUOTA_BANDWIDTH,
					Max:  5,
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

func copyMap(m map[QuotaType]Quota) map[QuotaType]Quota {
	newMap := map[QuotaType]Quota{}
	for k, v := range m {
		newMap[k] = v
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

	tCol := a.mgoSession.DB("").C("pantahub_trails")
	if tCol == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
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

	var mostRecentDeviceTrails []trails.Trail
	err := tCol.Find(bson.M{"owner": owner}).Sort("-last-touched").Limit(5).All(&mostRecentDeviceTrails)

	if err != nil {
		rest.Error(w, "Error finding devices for summary "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	summary.TopDevices = make([]DeviceInfo, 0)

	for _, v := range mostRecentDeviceTrails {
		var dev devices.Device
		err = dCol.Find(bson.M{"owner": owner, "prn": v.Device}).One(&dev)
		if err != nil {
			rest.Error(w, "Error finding device for top device summary "+err.Error(),
				http.StatusInternalServerError)
			return
		}
		dInfo := DeviceInfo{}
		dInfo.Prn = v.Device
		dInfo.Message = "Device changed at " + v.LastTouched.String()
		dInfo.Type = "INFO"
		dInfo.Nick = dev.Nick
		dInfo.DeviceId = dev.Id
		summary.TopDevices = append(summary.TopDevices, dInfo)
	}

	summary.Prn = owner.(string)
	summary.Nick = r.Env["JWT_PAYLOAD"].(map[string]interface{})["nick"].(string)
	summary.Sub = SubscriptionInfo{
		PlanId:     "VIP",
		Billing:    STANDARD_PLANS["VIP"].Billing,
		QuotaStats: copyMap(STANDARD_PLANS["VIP"].Quotas),
	}

	deviceCount, err :=
		dCol.Find(bson.M{"owner": owner}).Count()

	if err != nil {
		rest.Error(w, "Error finding devices for summary "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	quota := summary.Sub.QuotaStats[QUOTA_DEVICES]
	quota.Actual = int64(deviceCount)
	summary.Sub.QuotaStats[QUOTA_DEVICES] = quota

	w.WriteJson(summary)
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *DashApp {

	app := new(DashApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{Logger: log.New(os.Stdout, "dash|", 0)})
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
