// Copyright 2020  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package subscriptions

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// App subscription rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	service       SubscriptionService
}

// SubscriptionReq subscription request
type SubscriptionReq struct {
	Subject utils.Prn              `json:"subject"`
	Plan    utils.Prn              `json:"plan"`
	Attrs   map[string]interface{} `json:"attrs"`
}

// get Get subscription of a token user
// @Summary Get subscription of a token user
// @Description Get subscription of a token user
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags subscriptions
// @Success 200
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /subscriptions [get]
func (s *App) get(w rest.ResponseWriter, r *rest.Request) {

	authInfo := utils.GetAuthInfo(r)

	if authInfo == nil {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	err := r.ParseForm()
	if err != nil {
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): processing list subscription request for user %s: %s\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "Error processing request ("+errID.Hex()+")", http.StatusInternalServerError)
		return
	}

	start := r.PathParam("start")
	var startInt int
	if start != "" {
		startInt, _ = strconv.Atoi(start)
	} else {
		startInt = 0
	}

	page := r.PathParam("page")
	var pageInt int
	if page != "" {
		pageInt, _ = strconv.Atoi(page)
	} else {
		pageInt = -1
	}

	if authInfo.CallerType == "USER" {
		subs, err := s.service.List(r.Context(), utils.Prn(authInfo.Caller), startInt, pageInt)

		if err != nil {
			errID := bson.NewObjectId()
			log.Printf("ERROR (%s): processing list subscription request for user %s: %s\n",
				errID.Hex(), authInfo.Caller, err.Error())
			utils.RestErrorWrapper(w, "Error processing request ("+errID.Hex()+")", http.StatusInternalServerError)
			return
		}

		err = w.WriteJson(subs)
		if err != nil {
			errID := bson.NewObjectId()
			log.Printf("ERROR (%s): writing JSON response: %s ", errID.Hex(), err.Error())
			utils.RestErrorWrapper(w, "Error processing request ("+errID.Hex()+")", http.StatusInternalServerError)
			return
		}
		return
	}

	// XXX: right now not implemented
	errID := bson.NewObjectId()
	log.Printf(
		"WARNING (%s): DEVICE/SERVICE  %s is using unsupported api method 'list subscriptios'\n",
		errID.Hex(),
		authInfo.Caller)

	utils.RestErrorWrapper(w, "NOT IMPLEMENTED ("+errID.Hex()+")", http.StatusNotImplemented)
}

// put Add a new subscription as a admin
// @Summary Add a new subscription as a admin
// @Description Add a new subscription as a admin
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags subscriptions
// @Param body body SubscriptionReq true "Subscription request"
// @Success 200
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /subscriptions [get]
func (s *App) put(w rest.ResponseWriter, r *rest.Request) {

	authInfo := utils.GetAuthInfo(r)

	if authInfo == nil {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	if !s.service.IsAdmin(authInfo.Caller) {
		utils.RestErrorWrapper(w, "You need to have admin role for subscriptin service", http.StatusForbidden)
		return
	}

	err := r.ParseForm()

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error parsing form 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "NOT IMPLEMENTED ("+errID.Hex()+")", http.StatusNotImplemented)
		return
	}

	req := SubscriptionReq{}
	err = r.DecodeJsonPayload(&req)

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("WARNING (%s): error parsing body as json in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "BAD REQUEST RECEIVED ("+errID.Hex()+")", http.StatusPreconditionFailed)
		return
	}

	sub, err := s.service.LoadBySubject(r.Context(), req.Subject)

	if err != nil && err != mongo.ErrNoDocuments {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error using database in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "INTERNAL ERROR ("+errID.Hex()+")", http.StatusInternalServerError)
		return
	}

	if sub == nil {
		sub, err = s.service.New(r.Context(), req.Subject, authInfo.Caller, req.Plan, req.Attrs)
	} else {
		err = sub.UpdatePlan(r.Context(), authInfo.Caller, req.Plan, req.Attrs)
	}

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error updating plan and attrs in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "INTERNAL ERROR ("+errID.Hex()+")", http.StatusInternalServerError)
		return
	}

	w.WriteJson(sub)
}

// MakeHandler make the api handler
func (s *App) MakeHandler() http.Handler {
	return s.API.MakeHandler()
}

// New create a new subscription rest application
func New(jwtMiddleware *jwt.JWTMiddleware, subscriptionService SubscriptionService, mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.service = subscriptionService
	app.API = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/subscriptions:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "subscription"})
	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
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
	app.API.Use(&utils.URLCleanMiddleware{})

	// no authentication ngeeded for /login
	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwtMiddleware,
	})

	app.API.Use(&utils.AuthMiddleware{})

	// /auth_status endpoints
	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/", app.get),
		rest.Put("/admin/subscription", app.put),
	)
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)
	return app
}
