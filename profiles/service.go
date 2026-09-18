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

package profiles

import (
	"log"
	"os"

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

// App define a new rest application for profiles
type App struct {
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
}

// New create a profiles rest application
func New(jwtConfig *jwtauth.Config,
	mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtConfig = jwtConfig
	app.mongoClient = mongoClient

	err := app.setIndexes()
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_profiles: " + err.Error())
		return nil
	}

	return app
}

// Mount registers profiles on the echo server.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/profiles"

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

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/profiles:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "profiles"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
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
		}),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.JWT(app.jwtConfig),
		echoutil.Auth(),
	)

	onlyUser := echoutil.UserTypeFilterMW(onlyUserFilter)
	g.GET("/", app.handleGetProfiles, onlyUser, echoutil.ScopeFilterMW(readProfileScopes))
	g.PUT("/", app.handlePostProfile, onlyUser, echoutil.ScopeFilterMW(writeProfileScopes))
	g.GET("/config/meta", app.handleGetGlobalMeta, echoutil.ScopeFilterMW(readProfileScopes))
	g.PUT("/config/meta", app.handlePutGlobalMeta, echoutil.ScopeFilterMW(writeProfileScopes))
	g.GET("/:nick", app.handleGetProfile, onlyUser, echoutil.ScopeFilterMW(readProfileScopes))
}
