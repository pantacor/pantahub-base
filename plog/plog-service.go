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

	jwtgo "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// App plog rest application
type App struct {
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
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
func (a *App) handleGetPlogPosts(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	collPlogPosts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_plogposts")

	if collPlogPosts == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Need to be logged in as USER/SESSION user to get trail summary", http.StatusForbidden)
	}

	plogPosts := make([]Post, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	cur, err := collPlogPosts.Find(ctx, bson.M{
		"owner": owner,
	}, findOptions)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error on fetching plogposts:"+err.Error(), http.StatusForbidden)
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Post{}
		err := cur.Decode(&result)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
		}
		plogPosts = append(plogPosts, result)
	}

	return echoutil.WriteJSON(c, http.StatusOK, plogPosts)
}

// New creates a new plog rest application
func New(jwtConfig *jwtauth.Config, mongoClient *mongo.Client) *App {

	app := new(App)
	app.jwtConfig = jwtConfig
	app.mongoClient = mongoClient

	return app
}

// Mount registers plog on the echo server.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/plog"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/plog:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "plog"}, prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{
				"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.JWT(app.jwtConfig),
	)

	g.GET("/posts", app.handleGetPlogPosts)
}
