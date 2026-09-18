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

package metrics

import (
	"log"
	"os"

	"github.com/labstack/echo/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

// App metrics rest application
type App struct {
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
}

// handleGetMetrics Get API metrics
// @Summary Get API metrics
// @Description Get API metrics
// @Accept  plain/text
// @Produce  plain/text
// @Tags metrics
// @Success 200
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /metrics [get]
func (a *App) handleGetMetrics(c *echo.Context) error {
	promhttp.Handler().ServeHTTP(c.Response(), c.Request())
	return nil
}

// New create a new metrics rest application
func New(jwtConfig *jwtauth.Config, mongoClient *mongo.Client) *App {
	return &App{jwtConfig: jwtConfig, mongoClient: mongoClient}
}

// Mount registers metrics on echo.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/metrics"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/metrics:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "metrics"}, prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "OPTIONS"},
			AllowedHeaders: []string{
				"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.JWT(app.jwtConfig),
	)

	g.GET("/", echoutil.ScopeFilter(
		[]utils.Scope{utils.Scopes.API, utils.Scopes.Metrics, utils.Scopes.ReadMetrics},
		app.handleGetMetrics))
}
