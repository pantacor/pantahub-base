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
	"log"
	"os"
	"strconv"
	"time"

	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
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
