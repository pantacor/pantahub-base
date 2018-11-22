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
package plog

// Plog offers a simple mean to share pvr repos with others.
// Similar to a blog you post your pvr repo with a title, some description text
// and tags/sections.
//
// Every Pantahub user gets a plog he can use at his discretion.
//
// AccessControl is either private or public. More advanced ACL features will
// be available later or for users of organization accounts.
//
import (
	"log"
	"net/http"
	"os"
	"time"

	jwt "github.com/fundapps/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type PlogApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
}

type PlogPost struct {
	Id          bson.ObjectId          `json:"id" bson:"_id"`
	Owner       string                 `json:"owner"`
	LastInSync  time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched time.Time              `json:"last-touched" bson:"last-touched"`
	json        map[string]interface{} `json:"json" bson:"json"`
}

type PvrRemote struct {
	RemoteSpec         string   `json:"pvr-spec"`         // the pvr remote protocol spec available
	JsonGetUrl         string   `json:"json-get-url"`     // where to pvr post stuff
	JsonKey            string   `json:"json-key"`         // what key is to use in post json [default: json]
	ObjectsEndpointUrl string   `json:"objects-endpoint"` // where to store/retrieve objects
	PostUrl            string   `json:"post-url"`         // where to post/announce new revisions
	PostFields         []string `json:"post-fields"`      // what fields require input
	PostFieldsOpt      []string `json:"post-fields-opt"`  // what optional fields are available [default: <empty>]
}

//
// ## GET /trails/summary
//   get summary of all trails by the calling owner.
func (a *PlogApp) handle_getplogposts(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	collPlogPosts := a.mgoSession.DB("").C("pantahub_plogposts")

	if collPlogPosts == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get trail summary", http.StatusForbidden)
		return
	}

	plogPosts := make([]PlogPost, 0)
	collPlogPosts.Find(bson.M{"owner": owner}).All(&plogPosts)

	w.WriteJson(plogPosts)
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *PlogApp {

	app := new(PlogApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	app.Api = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/plog:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "plog"})
	app.Api.Use(rest.DefaultCommonStack...)

	// we allow calls from other domains to allow webapps; XXX: review
	app.Api.Use(&rest.CorsMiddleware{
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
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwt_middleware,
	})

	// /auth_status endpoints
	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	api_router, _ := rest.MakeRouter(
		//rest.Get("/", app.handle_getploginfo),
		rest.Get("/posts", app.handle_getplogposts),
	//	rest.Post("/posts", app.handle_postplogposts),
	)
	app.Api.SetApp(api_router)

	return app
}
