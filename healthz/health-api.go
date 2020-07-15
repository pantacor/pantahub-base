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
	"sync"

	"context"
	"log"
	"os"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

var (
	m                sync.Mutex
	lastResponse     Response
	lastResponseTime time.Time
)

// App health rest application
type App struct {
	API         *rest.Api
	mongoClient *mongo.Client
}

// Response helth response
type Response struct {
	ErrorCode int           `json:"code"`
	Duration  time.Duration `json:"duration"`
	Start     time.Time     `json:"start-time"`
}

// handleHealthz Get information of the health of the api services
// @Summary Get information of the health of the api services
// @Description Get information of the health of the api services
// @Accept  json
// @Produce  json
// @Security BasicAuth
// @Tags health
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {object} Response
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /healthz [get]
func (a *App) handleHealthz(w rest.ResponseWriter, r *rest.Request) {
	m.Lock()
	defer m.Unlock()

	if time.Now().Before(lastResponseTime.Add(30 * time.Second)) {
		w.WriteJson(lastResponse)
		return
	}

	response := Response{}

	response.Start = time.Now()

	user := r.Env["REMOTE_USER"].(string)

	if user == "" {
		utils.RestErrorWrapper(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	// check DB
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	val := map[string]interface{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, bson.M{}).Decode(&val)
	if err != nil && err != mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "Error with Database query:"+err.Error(), http.StatusInternalServerError)
		return
	}

	end := time.Now()
	response.Duration = end.Sub(response.Start)

	lastResponse = response
	lastResponseTime = time.Now()

	w.WriteJson(response)
}

// New create a new rest application
func New(mongoClient *mongo.Client) *App {

	app := new(App)
	app.mongoClient = mongoClient

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/health:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "health"})

	app.API.Use(rest.DefaultCommonStack...)

	saAdminSecret := utils.GetEnv(utils.EnvPantahubSaAdminSecret)

	basicAuthMW := &rest.AuthBasicMiddleware{
		Realm: "Pantahub Health @ " + utils.GetEnv(utils.EnvPantahubAuth),
		Authenticator: func(userId string, password string) bool {
			return saAdminSecret != "" && userId == "saadmin" && password == saAdminSecret
		},
	}

	// no authentication needed for /login
	app.API.Use(basicAuthMW)

	// /auth_status endpoints
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/", app.handleHealthz),
	)
	app.API.SetApp(apiRouter)

	return app
}
