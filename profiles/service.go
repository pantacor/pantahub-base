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

package profiles

import (
	"log"
	"os"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/mongo"
)

// App define a new rest application for profiles
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// New create a profiles rest application
func New(jwtMiddleware *jwt.JWTMiddleware,
	mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	err := app.setIndexes()
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_profiles: " + err.Error())
		return nil
	}
	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/profiles:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "profiles"})
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

	readProfileScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Profile,
		utils.Scopes.ReadProfile,
	}

	writeProfileScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Profile,
	}

	onlyUserFilter := []accounts.AccountType{
		accounts.AccountTypeUser,
	}

	apiRouter, _ := rest.MakeRouter(
		rest.Get(
			"/",
			rest.WrapMiddlewares(
				[]rest.Middleware{
					utils.InitUserTypeFilterMiddleware(onlyUserFilter),
					utils.InitScopeFilterMiddleware(readProfileScopes),
				},
				app.handleGetProfiles,
			),
		),
		rest.Put(
			"/",
			rest.WrapMiddlewares(
				[]rest.Middleware{
					utils.InitUserTypeFilterMiddleware(onlyUserFilter),
					utils.InitScopeFilterMiddleware(writeProfileScopes),
				},
				app.handlePostProfile,
			),
		),
		rest.Get(
			"/config/meta",
			rest.WrapMiddlewares(
				[]rest.Middleware{
					utils.InitScopeFilterMiddleware(readProfileScopes),
				},
				app.handleGetGlobalMeta,
			),
		),
		rest.Put(
			"/config/meta",
			rest.WrapMiddlewares(
				[]rest.Middleware{
					utils.InitScopeFilterMiddleware(readProfileScopes),
				},
				app.handlePutGlobalMeta,
			),
		),
		rest.Get(
			"/#nick",
			rest.WrapMiddlewares(
				[]rest.Middleware{
					utils.InitUserTypeFilterMiddleware(onlyUserFilter),
					utils.InitScopeFilterMiddleware(readProfileScopes),
				},
				app.handleGetProfile,
			),
		),
	)

	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
}
