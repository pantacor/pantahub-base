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

package exports

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
)

// PantahubDevicesAutoTokenV1 device auto token name
//
// #nosec G101 -- the name of an HTTP header, not a token value
const PantahubDevicesAutoTokenV1 = "Pantahub-Devices-Auto-Token-V1"
const CreateIndexTimeout = 600 * time.Second

// App Web app structure
type App struct {
	jwtConfig   *jwtauth.Config
	anonToken   func() string
	mongoClient *mongo.Client
	// objectServer stores object content; see SetObjectServer.
	objectServer http.Handler
}

// Build factory a new Device App only with mongoClient
func Build(mongoClient *mongo.Client) *App {
	return &App{
		mongoClient: mongoClient,
	}
}

// New create exports app. anonToken mints the token used for requests
// without credentials.
func New(jwtConfig *jwtauth.Config, anonToken func() string, mongoClient *mongo.Client) *App {
	return &App{jwtConfig: jwtConfig, anonToken: anonToken, mongoClient: mongoClient}
}

// Mount registers exports on echo with its previous middleware stack.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/exports"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/exports:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "exports"}, prefix),
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
		bearerOrAnon(echoutil.JWT(app.jwtConfig), app.anonToken),
		// Auth resolves the caller ScopeFilter checks; without it every
		// export of an authenticated user answers 401.
		echoutil.Auth(),
	)

	readDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	}

	// A link is its own credential; see links.go.
	g.GET("/links/:token/:filename", app.handleGetExportLink)
	g.PUT("/uploads/:token", app.handlePutExportUpload)
	g.GET("/:owner/:nick/:rev/:filename", echoutil.ScopeFilter(readDevicesScopes, app.handleGetExport))
}

// bearerOrAnon runs jwt for bearer requests and for requests without
// credentials, which get an anonymous token first. Other schemes pass through.
func bearerOrAnon(jwt echo.MiddlewareFunc, anonToken func() string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		withJWT := jwt(next)
		return func(c *echo.Context) error {
			h := c.Request().Header
			auth := h.Get("Authorization")
			if auth == "" {
				h.Set("Authorization", fmt.Sprintf("Bearer %s", anonToken()))
				return withJWT(c)
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth)), "bearer ") {
				return withJWT(c)
			}
			return next(c)
		}
	}
}
