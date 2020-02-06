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
// @Success 200 {object} PvrRemote
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/.prvremote [get]
func (a *App) handleGetStepPvrInfo(w rest.ResponseWriter, r *rest.Request) {
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
	revID := r.PathParam("rev")
	stepID := getID + "-" + revID
	step := Step{}

	isPublic, err := a.isTrailPublic(getID)
	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"owner":   owner,
			"_id":     stepID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	if err == mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "No access to device step trail "+stepID, http.StatusForbidden)
		return
	}

	if err != nil {
		utils.RestErrorWrapper(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oe := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" +
		revID + "/objects")

	jsonURL := utils.GetAPIEndpoint("/trails/" + getID + "/steps/" +
		revID + "/state")

	postURL := utils.GetAPIEndpoint("/trails/" + getID + "/steps")
	postFields := []string{"msg"}
	postFieldsOpt := []string{}

	remoteInfo := PvrRemote{
		RemoteSpec:         "pvr-pantahub-1",
		JSONGetURL:         jsonURL,
		ObjectsEndpointURL: oe,
		JSONKey:            "state",
		PostURL:            postURL,
		PostFields:         postFields,
		PostFieldsOpt:      postFieldsOpt,
	}

	w.WriteJson(remoteInfo)
}
