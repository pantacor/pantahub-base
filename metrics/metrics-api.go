//
// Copyright 2016-2019  Pantacor Ltd.
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
	"net/http"
	"os"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

type MetricsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mongoClient    *mongo.Client
}

type RestRequestResponseAdapter struct {
	Request  *rest.Request
	Response rest.ResponseWriter
}

func (a *MetricsApp) handle_getmetrics(w rest.ResponseWriter, r *rest.Request) {
	var httpResp http.ResponseWriter = w

	handler := promhttp.Handler()
	handler.ServeHTTP(httpResp, r.Request)
}

func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *MetricsApp {
	app := new(MetricsApp)
	app.jwt_middleware = jwtMiddleware
	app.mongoClient = mongoClient

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/metrics:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "metrics"})

	app.Api.Use(rest.DefaultCommonStack...)
	app.Api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		// default api
		rest.Get("/", utils.ScopeFilter(
			[]utils.Scope{utils.Scopes.API, utils.Scopes.Metrics, utils.Scopes.ReadMetrics},
			app.handle_getmetrics),
		),
	)
	app.Api.SetApp(api_router)

	return app
}
