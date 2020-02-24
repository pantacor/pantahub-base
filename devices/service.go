//
// Copyright 2020  Pantacor Ltd.
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
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
)

// PantahubDevicesAutoTokenV1 device auto token name
const PantahubDevicesAutoTokenV1 = "Pantahub-Devices-Auto-Token-V1"

//DeviceNickRule : Device nick rule used to create/update a device nick
const DeviceNickRule = `(?m)^[a-zA-Z0-9_\-+%]+$`

// App Web app structure
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// ModelError error type
type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Device device structure
type Device struct {
	ID           primitive.ObjectID     `json:"id" bson:"_id"`
	Prn          string                 `json:"prn"`
	Nick         string                 `json:"nick"`
	Owner        string                 `json:"owner"`
	OwnerNick    string                 `json:"owner-nick,omitempty" bson:"-"`
	Secret       string                 `json:"secret,omitempty"`
	TimeCreated  time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified time.Time              `json:"time-modified" bson:"timemodified"`
	Challenge    string                 `json:"challenge,omitempty"`
	IsPublic     bool                   `json:"public"`
	UserMeta     map[string]interface{} `json:"user-meta" bson:"user-meta"`
	DeviceMeta   map[string]interface{} `json:"device-meta" bson:"device-meta"`
	Garbage      bool                   `json:"garbage" bson:"garbage"`
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

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "timemodified", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}
	// Indexing for the owner,garbage fields
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}
	// Indexing for the device,garbage fields
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	err = app.EnsureTokenIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub devices tokens: " + err.Error())
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
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
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
		rest.Post("/register", utils.ScopeFilter(readDevicesScopes, app.handleRegister)),
		rest.Post("/issue/:service", utils.ScopeFilter(updateDevicesScopes, app.handleIssueDeviceCert)),

		// token api
		rest.Post("/tokens", utils.ScopeFilter(readDevicesScopes, app.handlePostTokens)),
		rest.Delete("/tokens/:id", utils.ScopeFilter(updateDevicesScopes, app.handleDisableTokens)),
		rest.Get("/tokens", utils.ScopeFilter(readDevicesScopes, app.handleGetTokens)),

		// default api
		rest.Get("/auth_status", utils.ScopeFilter(readDevicesScopes, handleAuth)),
		rest.Get("/", utils.ScopeFilter(readDevicesScopes, app.handleGetDevices)),
		rest.Post("/", utils.ScopeFilter(writeDevicesScopes, app.handlePostDevice)),
		rest.Get("/:id", utils.ScopeFilter(readDevicesScopes, app.handleDetDevice)),
		rest.Put("/:id", utils.ScopeFilter(writeDevicesScopes, app.handlePutDevice)),
		rest.Patch("/:id", utils.ScopeFilter(writeDevicesScopes, app.handlePatchDevice)),
		rest.Put("/:id/public", utils.ScopeFilter(writeDevicesScopes, app.handlePutPublic)),
		rest.Delete("/:id/public", utils.ScopeFilter(writeDevicesScopes, app.handleDeletePublic)),
		rest.Put("/:id/user-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePutUserData)),
		rest.Patch("/:id/user-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePatchUserData)),
		rest.Put("/:id/device-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePutDeviceData)),
		rest.Patch("/:id/device-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePatchDeviceData)),
		rest.Delete("/:id", utils.ScopeFilter(writeDevicesScopes, app.handleDeleteDevice)),
		// lookup by nick-path (np)
		rest.Get("/np/:usernick/:devicenick", utils.ScopeFilter(readDevicesScopes, app.handleGetUserDevice)),
	)
	app.API.SetApp(apiRouter)

	return app
}
