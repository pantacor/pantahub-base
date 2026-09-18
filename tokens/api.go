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

// Package tokens provides the tokens managment infrastructure for pantahub.
package tokens

//
import (
	"context"
	"log"
	"os"
	"time"

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenendpoints"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenrepo"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenservice"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

// App logs rest application
type App struct {
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
	endpoints   *tokenendpoints.Endpoints
}

var (
	onlyUserFilter = []accounts.AccountType{
		accounts.AccountTypeUser,
	}
)

// New create a new tokens rest application
func New(jwtConfig *jwtauth.Config, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtConfig = jwtConfig
	app.mongoClient = mongoClient

	repo := tokenrepo.New(mongoClient)
	app.endpoints = tokenendpoints.New(tokenservice.New(repo))
	if err := repo.SetIndexes(); err != nil {
		log.Fatal("can't create indexes to tokens app: ", err)
		return nil
	}

	// idempotent: add a sha256 digest to any token still stored in plaintext.
	// The plaintext is kept until PANTAHUB_PURGE_PLAINTEXT_SECRETS is
	// enabled, so a rolling deploy from a build that verifies the plaintext
	// keeps working.
	migrateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if n, err := repo.MigratePlaintextSecrets(migrateCtx); err != nil {
		log.Fatal("can't migrate plaintext token secrets: ", err)
		return nil
	} else if n > 0 {
		log.Printf("tokens: hashed %d plaintext token secrets", n)
	}
	if utils.GetEnv(utils.EnvPantahubPurgePlaintextSecrets) == "true" {
		if n, err := repo.PurgePlaintextSecrets(migrateCtx); err != nil {
			log.Fatal("can't purge plaintext token secrets: ", err)
			return nil
		} else if n > 0 {
			log.Printf("tokens: purged %d plaintext token secrets", n)
		}
	}

	return app
}

// Mount registers tokens on the echo server.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/tokens"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/tokens:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "tokens"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
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
	g.GET("/", app.endpoints.ListTokens)
	g.POST("/", app.endpoints.CreateToken, onlyUser)
	g.GET("/:id", app.endpoints.GetToken, onlyUser)
	g.DELETE("/:id", app.endpoints.DeleteToken, onlyUser)
}
