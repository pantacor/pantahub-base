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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"log"
	"os"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
)

// New create a new trails rest application
//
//	finish getsteps
//	post walk
//	get walks
//	search attributes for advanced steps/walk searching inside trail
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	// Indexing for the owner,garbage fields in pantahub_trails
	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_trails: " + err.Error())
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
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_trails: " + err.Error())
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
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
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
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	// INDEX FOR STEPS SEARCH BY OWNER
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "trail-id", Value: bsonx.Int32(1)},
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "progress.status", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
			{Key: "rev", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	// INDEX FOR STEPS SEARCH BY Device
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "trail-id", Value: bsonx.Int32(1)},
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "progress.status", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
			{Key: "rev", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	// INDEX FOR STEPS SEARCH BY Public
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "trail-id", Value: bsonx.Int32(1)},
			{Key: "progress.status", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
			{Key: "rev", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	app.API = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/trails:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "trails"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})
	app.API.Use(&utils.CanonicalJSONMiddleware{})

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
	app.API.Use(&utils.URLCleanMiddleware{})

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwtMiddleware,
	})
	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	// /auth_status endpoints
	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)

	readTrailsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.ReadTrails,
	}
	writeTrailsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.WriteTrails,
	}
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/auth_status", utils.ScopeFilter(readTrailsScopes, handleAuth)),
		rest.Get("/", utils.ScopeFilter(readTrailsScopes, app.handleGetTrails)),
		rest.Post("/", utils.ScopeFilter(writeTrailsScopes, app.handlePostTrail)),
		rest.Get("/summary", utils.ScopeFilter(readTrailsScopes, app.handleGetTrailSummary)),
		rest.Get("/#id", utils.ScopeFilter(readTrailsScopes, app.handleGetTrail)),
		rest.Get("/#id/.pvrremote", utils.ScopeFilter(readTrailsScopes, app.handleGetTrailPvrInfo)),
		rest.Get("/#id/steps", utils.ScopeFilter(readTrailsScopes, app.handleGetSteps)),
		rest.Post("/#id/steps", utils.ScopeFilter(writeTrailsScopes, app.handlePostStep)),
		rest.Get("/#id/steps/#rev", utils.ScopeFilter(readTrailsScopes, app.handleGetStep)),
		rest.Get("/#id/steps/#rev/.pvrremote", utils.ScopeFilter(readTrailsScopes, app.handleGetStepPvrInfo)),
		rest.Get("/#id/steps/#rev/meta", utils.ScopeFilter(readTrailsScopes, app.handleGetStepMeta)),
		rest.Get("/#id/steps/#rev/state", utils.ScopeFilter(readTrailsScopes, app.handleGetStepState)),
		rest.Get("/#id/steps/#rev/objects", utils.ScopeFilter(readTrailsScopes, app.handleGetStepsObjects)),
		rest.Get("/#id/steps/#rev/objects/#obj", utils.ScopeFilter(readTrailsScopes, app.handleGetStepsObject)),
		rest.Get("/#id/steps/#rev/objects/#obj/blob", utils.ScopeFilter(readTrailsScopes, app.handleGetStepsObjectFile)),
		rest.Post("/#id/steps/#rev/objects", utils.ScopeFilter(writeTrailsScopes, app.handlePostStepsObject)),
		rest.Put("/#id/steps/#rev/meta", utils.ScopeFilter(writeTrailsScopes, app.handlePutStepMeta)),
		rest.Put("/#id/steps/#rev/state", utils.ScopeFilter(writeTrailsScopes, app.handlePutStepState)),
		rest.Put("/#id/steps/#rev/progress", utils.ScopeFilter(writeTrailsScopes, app.handlePutStepProgress)),
		rest.Put("/#id/steps/#rev/cancel", utils.ScopeFilter(writeTrailsScopes, app.handlePutStepProgressCancel)),
		rest.Get("/#id/summary", utils.ScopeFilter(readTrailsScopes, app.handleGetTrailStepSummary)),
	)
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
}
