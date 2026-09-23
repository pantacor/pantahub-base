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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
package trails

import (
	"net/http"
	"strconv"
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (a *App) handleGetTrailPvrInfo(c *echo.Context) error {
	var err error

	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	getID := c.Param("id")
	step := trailmodels.Step{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	isPublic, err := a.isTrailPublic(ctx, getID)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error getting trail public", http.StatusInternalServerError)
	}
	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	trailObjectID, err := primitive.ObjectIDFromHex(getID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
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
		return echoutil.RestErrorWrapper(c, "No access to device trail "+trailObjectID.Hex(), http.StatusForbidden)
	}

	if err != nil {
		return echoutil.RestErrorWrapper(c, "No access to resource: "+err.Error(), http.StatusInternalServerError)
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

	return echoutil.WriteJSON(c, http.StatusOK, remoteInfo)
}
