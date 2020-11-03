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

package callbacks

import (
	"log"
	"os"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

// App define a new rest application for profiles
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// Build factory a new Callback App only with mongoClient
func Build(mongoClient *mongo.Client) *App {
	return &App{
		mongoClient: mongoClient,
	}
}

// New create a callbacks rest application
func New(jwtMiddleware *jwt.JWTMiddleware,
	mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/callbacks:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "callbacks"})

	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"PUT"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	saAdminSecret := utils.GetEnv(utils.EnvPantahubSaAdminSecret)

	basicAuthMW := &rest.AuthBasicMiddleware{
		Realm: "Pantahub Health @ " + utils.GetEnv(utils.EnvPantahubAuth),
		Authenticator: func(userId string, password string) bool {
			return saAdminSecret != "" && userId == "saadmin" && password == saAdminSecret
		},
	}

	// Using basic authentication for /callbacks
	app.API.Use(basicAuthMW)

	// end points
	apiRouter, _ := rest.MakeRouter(
		rest.Put("/devices/#id", app.handlePutDevice),
		rest.Put("/steps/#id", app.handlePutStep),
	)
	app.API.SetApp(apiRouter)

	return app
}
