//
// Copyright (c) 2017-2023 Pantacor Ltd.
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
	"strconv"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// handleGetTrail Get last step pvr remote information for a trail revision
// @Summary Get last step pvr remote information for a trail revision
// @Description Get last step pvr remote information for a trail revision
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Success 200 {object} trailmodels.PvrRemote
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/.pvrremote [get]
func (a *App) handleGetTrailPvrInfo(w rest.ResponseWriter, r *rest.Request) {
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

	getID := r.PathParam("id")
	step := trailmodels.Step{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	isPublic, err := a.isTrailPublic(ctx, getID)

	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	trailObjectID, err := primitive.ObjectIDFromHex(getID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	findOneOptions := options.FindOne()
	findOneOptions.SetSort(bson.M{"rev": -1})
	//	get last step
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"trail-id": trailObjectID,
			"garbage":  bson.M{"$ne": true},
		}, findOneOptions).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"device":   owner,
			"trail-id": trailObjectID,
			"garbage":  bson.M{"$ne": true},
		}, findOneOptions).Decode(&step)
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctx, bson.M{
			"owner":    owner,
			"trail-id": trailObjectID,
			"garbage":  bson.M{"$ne": true},
		}, findOneOptions).Decode(&step)
	}

	if err == mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "No access to device trail "+trailObjectID.Hex(), http.StatusForbidden)
		return
	}

	if err != nil {
		utils.RestErrorWrapper(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oe := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" + strconv.Itoa(step.Rev) + "/objects")
	jsonGet := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" + strconv.Itoa(step.Rev) + "/state")
	postURL := utils.GetAPIEndpoint("/trails/" + getID + "/steps")
	stepGetUrl := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" + strconv.Itoa(step.Rev))
	postFields := []string{"commit-msg"}
	postFieldsOpt := []string{"rev"}

	remoteInfo := trailmodels.PvrRemote{
		RemoteSpec:         "pvr-pantahub-1",
		JSONGetURL:         jsonGet,
		ObjectsEndpointURL: oe,
		JSONKey:            "state",
		PostURL:            postURL,
		PostFields:         postFields,
		PostFieldsOpt:      postFieldsOpt,
		StepGetUrl:         stepGetUrl,
	}

	w.WriteJson(remoteInfo)
}
