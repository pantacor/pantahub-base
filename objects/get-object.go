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

package objects

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"gopkg.in/mgo.v2/bson"
)

// handleGetObjects Get all object of the token owner
// @Summary Get all object of the token owner
// @Description Get all object of the token owner
// @Accept  json
// @Produce  json
// @Tags objects
// @Security ApiKeyAuth
// @Success 200 {array} Object
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /objects [get]
func (a *App) handleGetObjects(c *echo.Context) error {

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in as a USER", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	filter := c.Request().URL.Query().Get("filter")
	m := map[string]interface{}{}

	if filter != "" {
		err := json.Unmarshal([]byte(filter), &m)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error parsing filter json "+err.Error(), http.StatusBadRequest)
		}
		if err := mongoutils.ValidateClientFilter(m); err != nil {
			return echoutil.RestErrorWrapper(c, "Illegal filter: "+err.Error(), http.StatusBadRequest)
		}
	}
	m["owner"] = owner
	m["garbage"] = bson.M{"$ne": true}

	newObjects := make([]Object, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error on fetching objects:"+err.Error(), http.StatusForbidden)
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Object{}
		err := cur.Decode(&result)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
		}
		newObjects = append(newObjects, result)
	}

	return echoutil.WriteJSON(c, http.StatusOK, newObjects)
}
