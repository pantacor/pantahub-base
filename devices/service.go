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

package devices

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/caclient"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// PantahubDevicesAutoTokenV1 device auto token name
const PantahubDevicesAutoTokenV1 = "Pantahub-Devices-Auto-Token-V1"
const CreateIndexTimeout = 600 * time.Second

// DeviceNickRule : Device nick rule used to create/update a device nick
const DeviceNickRule = `(?m)^[a-zA-Z0-9_\-+%]+$`

// App Web app structure
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// Build factory a new Device App only with mongoClient
func Build(mongoClient *mongo.Client) *App {
	return &App{
		mongoClient: mongoClient,
	}
}

// ModelError error type
type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Device device structure
type Device struct {
	ID                  primitive.ObjectID     `json:"id" bson:"_id"`
	Prn                 string                 `json:"prn"`
	Nick                string                 `json:"nick"`
	Owner               string                 `json:"owner"`
	OwnerNick           string                 `json:"owner-nick,omitempty" bson:"-"`
	Secret              string                 `json:"secret,omitempty"`
	TimeCreated         time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified        time.Time              `json:"time-modified" bson:"timemodified"`
	Challenge           string                 `json:"challenge,omitempty"`
	IsPublic            bool                   `json:"public" bson:"ispublic"`
	UserMeta            map[string]interface{} `json:"user-meta" bson:"user-meta"`
	DeviceMeta          map[string]interface{} `json:"device-meta" bson:"device-meta"`
	Garbage             bool                   `json:"garbage" bson:"garbage"`
	MarkPublicProcessed bool                   `json:"mark_public_processed" bson:"mark_public_processed"`
}

type autoTokenInfo struct {
	Owner    string
	UserMeta map[string]interface{}
}

// New create devices web app
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	_, err := caclient.GetDefaultCAClient()
	if err != nil {
		if err, ok := err.(*caclient.ClientError); ok {
			if err.Code != caclient.ErrorNotConfig {
				log.Fatalf("Error loading caclient. Error Code: %d -- %s", err.Code, err.Error())
				return nil
			}
		}
	}

	err = app.EnsureDevicesIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub_devices: " + err.Error())
		return nil
	}

	err = app.EnsureTokenIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/devices:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "devices"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})

	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept",
			"Content-Type",
			"Content-Length",
			"X-Custom-Header",
			"Origin",
			"Authorization",
			"X-Trace-ID",
			"Trace-Id",
			"x-request-id",
			"X-Request-ID",
			"TraceID",
			"ParentID",
			"Uber-Trace-ID",
			"uber-trace-id",
			"traceparent",
			"tracestate",
		},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// if call is coming with authorization attempt, ensure JWT middleware
			// is used... otherwise let through anonymous POST for registration
			auth := request.Header.Get("Authorization")
			if auth != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth)), "bearer ") {
				return true
			}

			// post new device means to register... allow this unauthenticated
			return !((request.Method == "POST" && request.URL.Path == "/") ||
				(request.Method == "POST" && request.URL.Path == "/register"))
		},
		IfTrue: app.jwtMiddleware,
	})
	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// if call is coming with authorization attempt, ensure JWT middleware
			// is used... otherwise let through anonymous POST for registration
			auth := request.Header.Get("Authorization")
			if auth != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth)), "bearer ") {
				return true
			}

			// post new device means to register... allow this unauthenticated
			return !((request.Method == "POST" && request.URL.Path == "/") ||
				(request.Method == "POST" && request.URL.Path == "/register"))
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	writeDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.WriteDevices,
	}
	readDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	}
	updateDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.UpdateDevices,
	}

	// /auth_status endpoints
	apiRouter, _ := rest.MakeRouter(
		// TPM auto enroll register
		rest.Post("/register", app.handleRegister),

		// token api
		rest.Post("/tokens", utils.ScopeFilter(readDevicesScopes, app.handlePostTokens)),
		rest.Delete("/tokens/#id", utils.ScopeFilter(updateDevicesScopes, app.handleDisableTokens)),
		rest.Get("/tokens", utils.ScopeFilter(readDevicesScopes, app.handleGetTokens)),

		// default api
		rest.Get("/auth_status", utils.ScopeFilter(readDevicesScopes, handleAuth)),
		rest.Get("/", utils.ScopeFilter(readDevicesScopes, app.handleGetDevices)),
		rest.Post("/", utils.ScopeFilter(writeDevicesScopes,
			func(writer rest.ResponseWriter, request *rest.Request) {
				userAgent := request.Header.Get("User-Agent")
				if userAgent == "" {
					utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
					return
				}
				app.handlePostDevice(writer, request)
			})),
		rest.Get("/#id", utils.ScopeFilter(readDevicesScopes, app.handleGetDevice)),
		rest.Put("/#id", utils.ScopeFilter(writeDevicesScopes, app.handlePutDevice)),
		rest.Patch("/#id", utils.ScopeFilter(writeDevicesScopes, app.handlePatchDevice)),
		rest.Put("/#id/public", utils.ScopeFilter(writeDevicesScopes, app.handlePutPublic)),
		rest.Delete("/#id/public", utils.ScopeFilter(writeDevicesScopes, app.handleDeletePublic)),
		rest.Get("/#id/user-meta", utils.ScopeFilter(readDevicesScopes, app.handleGetUserData)),
		rest.Put("/#id/user-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePutUserData)),
		rest.Patch("/#id/user-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePatchUserData)),
		rest.Put("/#id/device-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePutDeviceData)),
		rest.Patch("/#id/device-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePatchDeviceData)),
		rest.Delete("/#id", utils.ScopeFilter(writeDevicesScopes, app.handleDeleteDevice)),
		// lookup by nick-path (np)
		rest.Get("/np/#usernick/#devicenick", utils.ScopeFilter(readDevicesScopes, app.handleGetUserDevice)),
	)
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
}
