//
// Package subscriptions offers simple subscription REST API to issue subscriptions
// for services. In this file we define the rest frontend.
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//
package subscriptions

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/utils"
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
		startInt, err = strconv.Atoi(start)
	} else {
		startInt = 0
	}

	page := r.PathParam("page")
	var pageInt int
	if page != "" {
		pageInt, err = strconv.Atoi(page)
	} else {
		pageInt = -1
	}

	if authInfo.CallerType == "USER" {
		subs, err := s.service.List(utils.Prn(authInfo.Caller), startInt, pageInt)

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
	log.Printf("WARNING (%s): DEVICE/SERVICE  %s is using unsupported api method 'list subscriptios'\n",
		errID.Hex(), authInfo.Caller)
	utils.RestErrorWrapper(w, "NOT IMPLEMENTED ("+errID.Hex()+")", http.StatusNotImplemented)
	return

}

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

	sub, err := s.service.LoadBySubject(req.Subject)

	if err != nil && err != mongo.ErrNoDocuments {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error using database in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "INTERNAL ERROR ("+errID.Hex()+")", http.StatusInternalServerError)
		return
	}

	if sub == nil {
		sub, err = s.service.New(req.Subject, authInfo.Caller, req.Plan, req.Attrs)
	} else {
		err = sub.UpdatePlan(authInfo.Caller, req.Plan, req.Attrs)
	}

	if err != nil {
		// XXX: right now not implemented
		errID := bson.NewObjectId()
		log.Printf("ERROR (%s): error updating plan and attrs in 'post subscriptions' by user %s: %s'\n",
			errID.Hex(), authInfo.Caller, err.Error())
		utils.RestErrorWrapper(w, "INTERNAL ERROR ("+errID.Hex()+")", http.StatusInternalServerError)
		return
	}
	return
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
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
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
	app.API.SetApp(apiRouter)
	return app
}
