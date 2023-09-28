// Copyright 2020  Pantacor Ltd.
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

	jwtgo "github.com/dgrijalva/jwt-go"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handleGetObjects(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	filter := r.URL.Query().Get("filter")
	m := map[string]interface{}{}

	if filter != "" {
		err := json.Unmarshal([]byte(filter), &m)
		if err != nil {
			utils.RestErrorWrapper(w, "Error parsing filter json "+err.Error(), http.StatusInternalServerError)
		}
	}
	m["owner"] = owner
	m["garbage"] = bson.M{"$ne": true}

	newObjects := make([]Object, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching objects:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Object{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		newObjects = append(newObjects, result)
	}

	w.WriteJson(newObjects)
}
