// Copyright (c) 2017-2026 Pantacor Ltd.
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

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// App subscription rest application
type App struct {
	jwtConfig   *jwtauth.Config
	service     SubscriptionService
	mongoClient *mongo.Client
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
func (s *App) get(c *echo.Context) error {

	authInfo := echoutil.AuthInfo(c)

	if authInfo == nil {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	err := c.Request().ParseForm()
	if err != nil {
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): processing list subscription request for user %s: %s\n",
			errID.Hex(), authInfo.Caller, err.Error())
		return echoutil.RestErrorWrapper(c, "Error processing request ("+errID.Hex()+")", http.StatusInternalServerError)
	}

	start := c.Param("start")
	var startInt int
	if start != "" {
		startInt, _ = strconv.Atoi(start)
	} else {
		startInt = 0
	}

	page := c.Param("page")
	var pageInt int
	if page != "" {
		pageInt, _ = strconv.Atoi(page)
	} else {
		pageInt = -1
	}

	if authInfo.CallerType == "USER" {
		subs, err := s.service.List(c.Request().Context(), utils.Prn(authInfo.Caller), startInt, pageInt)

		if err != nil {
			errID := bson.NewObjectId()
			log.Printf("ERROR (%s): processing list subscription request for user %s: %s\n",
				errID.Hex(), authInfo.Caller, err.Error())
			return echoutil.RestErrorWrapper(c, "Error processing request ("+errID.Hex()+")", http.StatusInternalServerError)
		}

		err = echoutil.WriteJSON(c, http.StatusOK, subs)
		if err != nil {
			errID := bson.NewObjectId()
			log.Printf("ERROR (%s): writing JSON response: %s ", errID.Hex(), err.Error())
			return echoutil.RestErrorWrapper(c, "Error processing request ("+errID.Hex()+")", http.StatusInternalServerError)
		}
		return nil
	}

	// XXX: right now not implemented
	errID := bson.NewObjectId()
	log.Printf(
		"WARNING (%s): DEVICE/SERVICE  %s is using unsupported api method 'list subscriptios'\n",
		errID.Hex(),
		authInfo.Caller)

	return echoutil.RestErrorWrapper(c, "NOT IMPLEMENTED ("+errID.Hex()+")", http.StatusNotImplemented)
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
func (s *App) put(c *echo.Context) error {

	authInfo := echoutil.AuthInfo(c)

	if authInfo == nil {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	if !s.service.IsAdmin(authInfo) {
		return echoutil.RestErrorWrapper(c, "You need to have admin role for subscriptin service", http.StatusForbidden)
	}

	err := c.Request().ParseForm()

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error parsing form 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		return echoutil.RestErrorWrapper(c, "NOT IMPLEMENTED ("+errID.Hex()+")", http.StatusNotImplemented)
	}

	req := SubscriptionReq{}
	err = echoutil.DecodeJsonPayload(c, &req)

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("WARNING (%s): error parsing body as json in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		return echoutil.RestErrorWrapper(c, "BAD REQUEST RECEIVED ("+errID.Hex()+")", http.StatusPreconditionFailed)
	}

	sub, err := s.service.LoadBySubject(c.Request().Context(), req.Subject)

	if err != nil && err != mongo.ErrNoDocuments {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error using database in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		return echoutil.RestErrorWrapper(c, "INTERNAL ERROR ("+errID.Hex()+")", http.StatusInternalServerError)
	}

	if sub == nil {
		sub, err = s.service.New(c.Request().Context(), req.Subject, authInfo.Caller, req.Plan, req.Attrs)
	} else {
		err = sub.UpdatePlan(c.Request().Context(), authInfo.Caller, req.Plan, req.Attrs)
	}

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error updating plan and attrs in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		return echoutil.RestErrorWrapper(c, "INTERNAL ERROR ("+errID.Hex()+")", http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, sub)
}

// New create a new subscription rest application
func New(jwtConfig *jwtauth.Config, subscriptionService SubscriptionService, mongoClient *mongo.Client) *App {
	return &App{jwtConfig: jwtConfig, service: subscriptionService, mongoClient: mongoClient}
}

// Mount registers subscriptions on echo with its previous middleware stack.
func (s *App) Mount(srv *echoutil.Server) {
	const prefix = "/subscriptions"

	g := srv.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/subscriptions:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "subscription"}, prefix),
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
		echoutil.URLClean(),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: s.jwtConfig, Mongo: s.mongoClient}),
		echoutil.JWT(s.jwtConfig),
		echoutil.Auth(),
	)

	// URLClean strips the trailing "/", so the root is the bare prefix.
	g.GET("", s.get)
	g.PUT("/admin/subscription", s.put)
}
