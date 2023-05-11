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
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/trails/trailservices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

// handleGetStepsObjects Get trails step objects
// @Summary Get trails step objects
// @Description Get trails step objects
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Success 200 {object} objects.ObjectWithAccess
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/objects [get]
func (a *App) handleGetStepsObjects(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	trailID := r.PathParam("id")
	rev := r.PathParam("rev")

	isPublic, err := a.isTrailPublic(r.Context(), trailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
		return
	}

	trailservice := trailservices.CreateService(a.mongoClient, utils.MongoDb)
	objectsWithAccess, rerr := trailservice.GetTrailObjectsWithAccess(
		r.Context(),
		trailID,
		rev,
		owner.(string),
		authType.(string),
		isPublic,
		"",
	)

	if rerr != nil {
		utils.RestErrorWrite(w, rerr)
		return
	}

	w.WriteJson(&objectsWithAccess)
}

// handleGetStepsObject Get trails step object
// @Summary Get trails step object
// @Description Get trails step object
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Param object_id path string true "OBJECT_ID"
// @Success 200 {object} objects.ObjectWithAccess
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/objects/{object_id} [get]
func (a *App) handleGetStepsObject(w rest.ResponseWriter, r *rest.Request) {
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

	step := trailmodels.Step{}

	trailID := r.PathParam("id")
	rev := r.PathParam("rev")
	objIDParam := r.PathParam("obj")

	isPublic, err := a.isTrailPublic(r.Context(), trailID)
	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
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
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	stateU := utils.BsonUnquoteMap(&step.State)

	var objWithAccess *objects.ObjectWithAccess

	for k, v := range stateU {
		_, ok := v.(string)

		if !ok {
			// we found a json element
			continue
		}

		if k == "#spec" {
			continue
		}

		collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

		if collection == nil {
			utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		callingPrincipalStr, ok := owner.(string)
		if !ok {
			// XXX: find right error
			utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
			return
		}

		objID := v.(string)

		if objIDParam != objID {
			continue
		}

		sha, err := utils.DecodeSha256HexString(objID)

		if err != nil {
			utils.RestErrorWrapper(w, "Get Trails Steps Object id must be a valid sha256", http.StatusBadRequest)
			return
		}

		storageID := objects.MakeStorageID(step.Owner, sha)

		var newObject objects.Object
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		err = collection.FindOne(ctx, bson.M{
			"_id":     storageID,
			"garbage": bson.M{"$ne": true},
		}).
			Decode(&newObject)

		if err != nil {
			utils.RestErrorWrapper(w, "Not Accessible Resource Id: "+storageID+" ERR: "+err.Error(), http.StatusForbidden)
			return
		}

		if newObject.Owner != step.Owner {
			utils.RestErrorWrapper(w, "Invalid Object Access ("+newObject.Owner+":"+step.Owner+")", http.StatusForbidden)
			return
		}

		newObject.ObjectName = k

		issuerURL := utils.GetAPIEndpoint("/trails")

		tmp := objects.MakeObjAccessible(issuerURL, callingPrincipalStr, newObject, storageID)
		objWithAccess = &tmp

		if newObject.LinkedObject != "" {
			w.Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeLink)
		} else {
			w.Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeObject)
		}

		break
	}

	if objWithAccess != nil {
		w.WriteJson(&objWithAccess)
	} else {
		utils.RestErrorWrapper(w, "Invalid Object", http.StatusForbidden)
	}
}

// handleGetStepsObject Get trails step object content
// @Summary Get trails step object content
// @Description Get trails step object content
// @Accept  json
// @Produce  json
// @Tags trails
// @Security ApiKeyAuth
// @Param id path string true "ID|NICK|PRN"
// @Param rev path string true "REV_ID"
// @Param object_id path string true "OBJECT_ID"
// @Header 200 {string} Location "File location URL"
// @Success 200
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /trails/{id}/steps/{rev}/objects/{object_id} [get]
func (a *App) handleGetStepsObjectFile(w rest.ResponseWriter, r *rest.Request) {

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

	step := trailmodels.Step{}

	trailID := r.PathParam("id")
	rev := r.PathParam("rev")
	objIDParam := r.PathParam("obj")

	isPublic, err := a.isTrailPublic(r.Context(), trailID)

	if err != nil {
		utils.RestErrorWrapper(w, "Error getting trail public:"+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
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
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailID + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	stateU := utils.BsonUnquoteMap(&step.State)

	var objWithAccess *objects.ObjectWithAccess

	for k, v := range stateU {
		_, ok := v.(string)

		if !ok {
			// we found a json element
			continue
		}

		if k == "#spec" {
			continue
		}

		collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

		if collection == nil {
			utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		callingPrincipalStr, ok := owner.(string)
		if !ok {
			// XXX: find right error
			utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
			return
		}

		objID := v.(string)

		if objIDParam != objID {
			continue
		}

		sha, err := utils.DecodeSha256HexString(objID)

		if err != nil {
			utils.RestErrorWrapper(w, "Get Trails Steps Object File by ID must be a valid sha256", http.StatusBadRequest)
			return
		}

		storageID := objects.MakeStorageID(step.Owner, sha)

		var newObject objects.Object
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		err = collection.FindOne(ctx, bson.M{
			"_id":     storageID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&newObject)

		if err != nil {
			utils.RestErrorWrapper(w, "Not Accessible Resource Id: "+storageID+" ERR: "+err.Error(), http.StatusForbidden)
			return
		}

		if newObject.Owner != step.Owner {
			utils.RestErrorWrapper(w, "Invalid Object Access", http.StatusForbidden)
			return
		}

		newObject.ObjectName = k

		issuerURL := utils.GetAPIEndpoint("/trails")
		tmp := objects.MakeObjAccessible(issuerURL, callingPrincipalStr, newObject, storageID)
		objWithAccess = &tmp

		if newObject.LinkedObject != "" {
			w.Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeLink)
		} else {
			w.Header().Add(objects.HttpHeaderPantahubObjectType, objects.ObjectTypeObject)
		}
		break
	}

	if objWithAccess == nil {
		utils.RestErrorWrapper(w, "Invalid Object", http.StatusForbidden)
		return
	}

	url := objWithAccess.SignedGetURL
	w.Header().Add("Location", url)
	w.WriteHeader(http.StatusFound)
}
