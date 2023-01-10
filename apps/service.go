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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
)

const (
	// AppTypeConfidential define a confidential client for oauth
	AppTypeConfidential = "confidential"

	// AppTypePublic define a public client for oauth
	AppTypePublic = "public"

	// DBCollection db collection name for thirdparty apps
	DBCollection = "pantahub_apps"

	// Prn name convection for the prn
	Prn = "prn:::apps:/"
)

// TPApp OAuth App Type
type TPApp struct {
	ID                  primitive.ObjectID `json:"id" bson:"_id"`
	Name                string             `json:"name" bson:"name"`
	Logo                string             `json:"logo" bson:"logo"`
	Type                string             `json:"type" bson:"type"`
	Nick                string             `json:"nick" bson:"nick"`
	Prn                 string             `json:"prn" bson:"prn"`
	Owner               string             `json:"owner"`
	OwnerNick           string             `json:"owner-nick,omitempty" bson:"owner-nick,omitempty"`
	Secret              string             `json:"secret,omitempty" bson:"secret"`
	RedirectURIs        []string           `json:"redirect_uris,omitempty" bson:"redirect_uris,omitempty"`
	Scopes              []utils.Scope      `json:"scopes,omitempty" bson:"scopes,omitempty"`
	ExposedScopes       []utils.Scope      `json:"exposed_scopes,omitempty" bson:"exposed_scopes,omitempty"`
	ExposedScopesLength int                `bson:"exposed_scopes_length,omit"`
	TimeCreated         time.Time          `json:"time-created" bson:"time-created"`
	TimeModified        time.Time          `json:"time-modified" bson:"time-modified"`
	DeletedAt           *time.Time         `json:"deleted-at,omitempty" bson:"deleted-at,omitempty"`
}

// App thirdparty application manager
type App struct {
	API           *rest.Api
	jwtMiddleware *jwt.JWTMiddleware
	mongoClient   *mongo.Client
}

// New create a new thirparty apps manager api
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	err := app.setIndexes()
	if err != nil {
		log.Fatalln(err.Error())
		return nil
	}

	app.setupAPI()

	// Define router or service
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/scopes", app.handleGetPhScopes),
		rest.Post("/", app.handleCreateApp),
		rest.Get("/", app.handleGetApps),
		rest.Get("/#id", app.handleGetApp),
		rest.Put("/#id", app.handleUpdateApp),
		rest.Delete("/#id", app.handleDeleteApp),
	)
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
}

func needsAuth(request *rest.Request) bool {
	return request.URL.Path != "/scopes"
}

func (app *App) setupAPI() {
	app.API = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/apps:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "apps"})
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
			"X-Custom-Header",
			"Origin",
			"Authorization",
			"X-Trace-Id",
			"Trace-Id",
			"x-request-id",
			"X-Request-Id",
		},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})
	app.API.Use(&rest.IfMiddleware{
		Condition: needsAuth,
		IfTrue:    app.jwtMiddleware,
	})
	app.API.Use(&rest.IfMiddleware{
		Condition: needsAuth,
		IfTrue:    &utils.AuthMiddleware{},
	})
}

func (app *App) setIndexes() error {
	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection := app.mongoClient.Database(utils.MongoDb).Collection(DBCollection)
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", DBCollection, err.Error())
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection(DBCollection)
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", DBCollection, err.Error())
	}

	return nil
}
