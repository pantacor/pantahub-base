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
	"net/http"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

type meta map[string]interface{}

// handleGetStep Get step meta just the raw data of a step without metainfo
// @Summary Get step meta just the raw data of a step without metainfo
// @Description Get step meta just the raw data of a step without metainfo
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Success 200 {object} meta
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/meta [get]
func (a *App) handleGetStepMeta(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	trailID := r.PathParam("id")
	rev := r.PathParam("rev")

	isPublic, err := a.isTrailPublic(trailID)

	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	if step.Meta == nil {
		step.Meta = map[string]interface{}{}
	}

	w.WriteJson(utils.BsonUnquoteMap(&step.Meta))
}
