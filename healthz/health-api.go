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

package healthz

import (
	"net/http"
	"sync"

	"context"
	"crypto/subtle"
	"log"
	"os"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
	mongoClient *mongo.Client
}

// Response helth response
type Response struct {
	ErrorCode int           `json:"code"`
	Duration  time.Duration `json:"duration"`
	Start     time.Time     `json:"start-time"`
}

type responseDoc struct {
	ErrorCode int       `json:"code"`
	Duration  int64     `json:"duration"`
	Start     time.Time `json:"start-time"`
}

// handleHealthz Get information of the health of the api services
// @Summary Get information of the health of the api services
// @Description Get information of the health of the api services
// @Accept  json
// @Produce  json
// @Security BasicAuth
// @Tags health
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {object} responseDoc
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /healthz [get]
func (a *App) handleHealthz(c *echo.Context) error {
	m.Lock()
	defer m.Unlock()

	if time.Now().Before(lastResponseTime.Add(30 * time.Second)) {
		return echoutil.WriteJSON(c, http.StatusOK, lastResponse)
	}

	response := Response{}

	response.Start = time.Now()

	user, _ := c.Get(echoutil.KeyRemoteUser).(string)

	if user == "" {
		return echoutil.RestErrorWrapper(c, "Not authorized", http.StatusUnauthorized)
	}

	// check DB
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}
	val := map[string]interface{}{}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	err := collection.FindOne(ctx, bson.M{}).Decode(&val)
	if err != nil && err != mongo.ErrNoDocuments {
		return echoutil.RestErrorWrapper(c, "Error with Database query:"+err.Error(), http.StatusInternalServerError)
	}

	end := time.Now()
	response.Duration = end.Sub(response.Start)

	lastResponse = response
	lastResponseTime = time.Now()

	return echoutil.WriteJSON(c, http.StatusOK, response)
}

// New create a new rest application
func New(mongoClient *mongo.Client) *App {

	app := new(App)
	app.mongoClient = mongoClient

	return app
}

// Mount registers healthz on echo.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/healthz"

	saAdminSecret := utils.GetEnv(utils.EnvPantahubSaAdminSecret)
	basicAuthMW := echoutil.BasicAuthConfig{
		Realm: "Pantahub Health @ " + utils.GetEnv(utils.EnvPantahubAuth),
		Authenticator: func(userID string, password string) bool {
			return saAdminSecret != "" && userID == "saadmin" && subtle.ConstantTimeCompare([]byte(password), []byte(saAdminSecret)) == 1
		},
	}

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/health:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "health"}, prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.AuthBasic(basicAuthMW),
	)

	g.GET("/", app.handleHealthz)
}
