//
// Copyright 2017,2018  Pantacor Ltd.
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

// Package plog offers a simple mean to share pvr repos with others.
// Similar to a blog you post your pvr repo with a title, some description text
// and tags/sections.
//
// Every Pantahub user gets a plog he can use at his discretion.
//
// AccessControl is either private or public. More advanced ACL features will
// be available later or for users of organization accounts.
package plog

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
)

// App plog rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// Post plog post payload
type Post struct {
	ID          primitive.ObjectID     `json:"id" bson:"_id"`
	Owner       string                 `json:"owner"`
	LastInSync  time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched time.Time              `json:"last-touched" bson:"last-touched"`
	JSON        map[string]interface{} `json:"json" bson:"json"`
}

// PvrRemote remote PVR
type PvrRemote struct {
	RemoteSpec         string   `json:"pvr-spec"`         // the pvr remote protocol spec available
	JSONGetURL         string   `json:"json-get-url"`     // where to pvr post stuff
	JSONKey            string   `json:"json-key"`         // what key is to use in post json [default: json]
	ObjectsEndpointURL string   `json:"objects-endpoint"` // where to store/retrieve objects
	PostURL            string   `json:"post-url"`         // where to post/announce new revisions
	PostFields         []string `json:"post-fields"`      // what fields require input
	PostFieldsOpt      []string `json:"post-fields-opt"`  // what optional fields are available [default: <empty>]
}

// ## GET /trails/summary
//
//	get summary of all trails by the calling owner.
func (a *App) handleGetPlogPosts(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	collPlogPosts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_plogposts")

	if collPlogPosts == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER/SESSION user to get trail summary", http.StatusForbidden)
		return
	}

	plogPosts := make([]Post, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cur, err := collPlogPosts.Find(ctx, bson.M{
		"owner": owner,
	}, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching plogposts:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Post{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		plogPosts = append(plogPosts, result)
	}

	w.WriteJson(plogPosts)
}

// New creates a new plog rest application
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	app.API = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/plog:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "plog"})
	app.API.Use(rest.DefaultCommonStack...)

	// we allow calls from other domains to allow webapps; XXX: review
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

	// no authentication needed for /login
	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwtMiddleware,
	})

	// /auth_status endpoints
	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	apiRouter, _ := rest.MakeRouter(
		//rest.Get("/", app.handle_getploginfo),
		rest.Get("/posts", app.handleGetPlogPosts),
	//	rest.Post("/posts", app.handlePostPlogPosts),
	)
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
}
