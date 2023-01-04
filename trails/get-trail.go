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
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

// handleGetTrail Get a trail by ID
// @Summary Get a trail by ID
// @Description get one trail; owning devices and users with trail control for the device
// @Description can get a trail. If not found or if no access, NotFound status code is returned
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Success 200 {object} Trail
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id} [get]
func (a *App) handleGetTrail(w rest.ResponseWriter, r *rest.Request) {

	var err error

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

	getID := r.PathParam("id")
	trail := Trail{}

	isPublic, err := a.isTrailPublic(r.Context(), getID)

	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	trailObjectID, err := primitive.ObjectIDFromHex(getID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	}

	if err != nil {
		utils.RestErrorWrapper(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	trail.FactoryState = utils.BsonUnquoteMap(&trail.FactoryState)

	w.WriteJson(trail)
}
