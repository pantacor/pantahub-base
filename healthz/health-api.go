//
// Copyright 2017-2019  Pantacor Ltd.
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
package healthz

import (
	"net/http"

	"log"
	"os"
	"path"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/mongodb/mongo-go-driver/mongo"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

type HealthzApp struct {
	Api         *rest.Api
	mongoClient *mongo.Client
}

type Response struct {
	ErrorCode int           `json:"code"`
	Duration  time.Duration `json:"duration"`
	Start     time.Time     `json:"start-time"`
}

func (a *HealthzApp) handle_healthz(w rest.ResponseWriter, r *rest.Request) {

	response := Response{}

	response.Start = time.Now()

	user := r.Env["REMOTE_USER"].(string)

	if user == "" {
		rest.Error(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	// check DB
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	val := map[string]interface{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, bson.M{}).Decode(&val)
	if err != nil {
		rest.Error(w, "Error with Database query:"+err.Error(), http.StatusInternalServerError)
		return
	}
	// check storage
	s3Path := utils.GetEnv(utils.ENV_PANTAHUB_S3PATH)

	_, err = os.Stat(path.Join(s3Path, "HEALTHZ.txt"))

	if err != nil {
		log.Println("ERROR: getting stats of HEALTHZ.txt on local-s3 storage: " + err.Error())
		rest.Error(w, "Error getting stats of HEALTHZ.txt on local-s3 storage", http.StatusInternalServerError)
		return
	}

	end := time.Now()
	response.Duration = end.Sub(response.Start)

	w.WriteJson(response)
}

func New(mongoClient *mongo.Client) *HealthzApp {

	app := new(HealthzApp)
	app.mongoClient = mongoClient

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/health:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "health"})

	app.Api.Use(rest.DefaultCommonStack...)

	saAdminSecret := utils.GetEnv(utils.ENV_PANTAHUB_SA_ADMIN_SECRET)

	basicAuthMW := &rest.AuthBasicMiddleware{
		Realm: "Pantahub Health @ " + utils.GetEnv(utils.ENV_PANTAHUB_AUTH),
		Authenticator: func(userId string, password string) bool {
			return saAdminSecret != "" && userId == "saadmin" && password == saAdminSecret
		},
	}

	// no authentication needed for /login
	app.Api.Use(basicAuthMW)

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/", app.handle_healthz),
	)
	app.Api.SetApp(api_router)

	return app
}
