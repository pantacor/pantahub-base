//
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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"log"
	"net/http"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// handleGetTrails Get a list of trails
// @Summary Get a list of trails
// @Description devices get a list of one and only one trail. users get trails for all the
// @Description devices they have trail control over (right now simplified for owner)
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Success 200 {array} Trail
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails [get]
func (a *App) handleGetTrails(w rest.ResponseWriter, r *rest.Request) {

	initialState := map[string]interface{}{}

	r.DecodeJsonPayload(&initialState)

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ownerField := ""
	if authType == "DEVICE" {
		ownerField = "device"
	} else if authType == "USER" || authType == "SESSION" {
		ownerField = "owner"
	}

	trails := make([]Trail, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cur, err := coll.Find(ctx, bson.M{
		ownerField: owner,
		"garbage":  bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching devices:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := Trail{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		result.FactoryState = utils.BsonUnquoteMap(&result.FactoryState)
		trails = append(trails, result)
	}

	if authType == "DEVICE" {
		if len(trails) > 1 {
			log.Println("WARNING: more than one trail in db for device - bad DB: " + owner.(string))
			trails = trails[0:1]
		}
	}
	w.WriteJson(trails)
}
