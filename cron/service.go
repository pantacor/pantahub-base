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

package cron

import (
	"crypto/subtle"
	"log"
	"os"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

// App define a new rest application for profiles
type App struct {
	jwtConfig      *jwtauth.Config
	CronJobTimeout time.Duration
	mongoClient    *mongo.Client
}

// New create a callbacks rest application
func New(jwtConfig *jwtauth.Config,
	cronJobTimeout time.Duration,
	mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtConfig = jwtConfig
	app.CronJobTimeout = cronJobTimeout
	app.mongoClient = mongoClient

	return app
}

// Mount registers cron on the echo server.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/cron"

	saAdminSecret := utils.GetEnv(utils.EnvPantahubSaAdminSecret)
	basicAuthMW := echoutil.BasicAuthConfig{
		Realm: "Pantahub Health @ " + utils.GetEnv(utils.EnvPantahubAuth),
		Authenticator: func(userID string, password string) bool {
			return saAdminSecret != "" && userID == "saadmin" && subtle.ConstantTimeCompare([]byte(password), []byte(saAdminSecret)) == 1
		},
	}

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/cron:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "cron"}, prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"PUT"},
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
		echoutil.AuthBasic(basicAuthMW),
	)

	g.PUT("/public/devices", app.handlePutDevices)
	g.PUT("/public/steps", app.handlePutSteps)
}
