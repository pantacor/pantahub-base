//
// Copyright (c) 2017-2026 Pantacor Ltd.
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

	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// New create a new trails rest application
//
//	finish getsteps
//	post walk
//	get walks
//	search attributes for advanced steps/walk searching inside trail
//
// Build returns an App over mongoClient for the functions other packages
// share, without mounting routes or touching indexes.
func Build(mongoClient *mongo.Client) *App {
	return &App{mongoClient: mongoClient}
}

func New(jwtConfig *jwtauth.Config, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtConfig = jwtConfig
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
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "device", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "device", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "trail-id", Value: int32(1)},
			{Key: "owner", Value: int32(1)},
			{Key: "progress.status", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
			{Key: "rev", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "trail-id", Value: int32(1)},
			{Key: "device", Value: int32(1)},
			{Key: "progress.status", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
			{Key: "rev", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "trail-id", Value: int32(1)},
			{Key: "progress.status", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
			{Key: "rev", Value: int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	return app
}

// Mount registers trails on echo with its previous middleware stack.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/trails"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/trails:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "trails"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.CanonicalJSON(),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests:         false,
			OriginValidator:               echoutil.AllowAllOrigins,
			AllowedMethods:                []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:                []string{"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization", "Content-Length"},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		echoutil.URLClean(),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.JWT(app.jwtConfig),
		echoutil.Auth(),
	)

	read := echoutil.ScopeFilterMW([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.ReadTrails,
	})
	write := echoutil.ScopeFilterMW([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.WriteTrails,
	})

	// URLClean strips the trailing "/", so the root is the bare prefix.
	g.GET("/auth_status", handleAuth, read)
	g.GET("", app.handleGetTrails, read)
	g.POST("", app.handlePostTrail, write)
	g.GET("/summary", app.handleGetTrailSummary, read)
	g.GET("/:id", app.handleGetTrail, read)
	g.GET("/:id/.pvrremote", app.handleGetTrailPvrInfo, read)
	g.GET("/:id/steps", app.handleGetSteps, read)
	g.POST("/:id/steps", app.handlePostStep, write)
	g.GET("/:id/steps/:rev", app.handleGetStep, read)
	g.GET("/:id/steps/:rev/.pvrremote", app.handleGetStepPvrInfo, read)
	g.GET("/:id/steps/:rev/meta", app.handleGetStepMeta, read)
	g.GET("/:id/steps/:rev/state", app.handleGetStepState, read)
	g.GET("/:id/steps/:rev/objects", app.handleGetStepsObjects, read)
	g.GET("/:id/steps/:rev/objects/:obj", app.handleGetStepsObject, read)
	g.GET("/:id/steps/:rev/objects/:obj/blob", app.handleGetStepsObjectFile, read)
	g.POST("/:id/steps/:rev/objects", app.handlePostStepsObject, write)
	g.PUT("/:id/steps/:rev/meta", app.handlePutStepMeta, write)
	g.PUT("/:id/steps/:rev/state", app.handlePutStepState, write)
	g.PUT("/:id/steps/:rev/progress", app.handlePutStepProgress, write)
	g.PUT("/:id/steps/:rev/cancel", app.handlePutStepProgressCancel, write)
	g.PUT("/:id/steps/:rev/wontgo", app.handlePutStepProgressWontgo, write)
	g.GET("/:id/summary", app.handleGetTrailStepSummary, read)
}
