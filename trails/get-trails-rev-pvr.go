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
	"time"

	"context"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// handleGetStep Get pvr remote information for a trail revision
// @Summary Get pvr remote information for a trail revision
// @Description Get pvr remote information for a trail revision
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Success 200 {object} trailmodels.PvrRemote
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/.prvremote [get]
func (a *App) handleGetStepPvrInfo(c *echo.Context) error {
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
	revID := c.Param("rev")
	stepID := getID + "-" + revID
	step := trailmodels.Step{}

	isPublic, err := a.isTrailPublic(c.Request().Context(), getID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	//	get last step
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     stepID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)

	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"device":  owner,
			"_id":     stepID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctx, bson.M{
			"owner":   owner,
			"_id":     stepID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	if err == mongo.ErrNoDocuments {
		return echoutil.RestErrorWrapper(c, "No access to device step trail "+stepID, http.StatusForbidden)
	}

	if err != nil {
		return echoutil.RestErrorWrapper(c, "No access to resource: "+err.Error(), http.StatusInternalServerError)
	}

	oe := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" +
		revID + "/objects")

	jsonURL := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" +
		revID + "/state")

	postURL := utils.GetAPIEndpoint("/trails/" + getID + "/steps")
	stepGetUrl := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" + revID)
	postFields := []string{"msg"}
	postFieldsOpt := []string{}

	remoteInfo := trailmodels.PvrRemote{
		RemoteSpec:         "pvr-pantahub-1",
		JSONGetURL:         jsonURL,
		ObjectsEndpointURL: oe,
		JSONKey:            "state",
		PostURL:            postURL,
		PostFields:         postFields,
		PostFieldsOpt:      postFieldsOpt,
		StepGetUrl:         stepGetUrl,
	}

	return echoutil.WriteJSON(c, http.StatusOK, remoteInfo)
}
