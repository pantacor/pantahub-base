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

package trailservices

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pvr/libpvr"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

type TrailService interface {
	GetTrailObjectsWithAccess(ctx context.Context, deviceID, rev, owner, authType string, isPublic bool, frags string) (owa []objects.ObjectWithAccess, rerr *utils.RError)
	GetStepRev(ctx context.Context, trailID string, rev string) (*trailmodels.Step, *utils.RError)
}

type TService struct {
	storage *mongo.Client
	db      *mongo.Database
}

func CreateService(client *mongo.Client, db string) TrailService {
	return &TService{
		storage: client,
		db:      client.Database(db),
	}
}

func (s *TService) GetStepRev(ctx context.Context, trailID string, rev string) (step *trailmodels.Step, rerr *utils.RError) {
	coll := s.db.Collection("pantahub_steps")
	ctxi, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	findOneOptions := options.FindOne()
	findOneOptions.SetSort(bson.M{"rev": -1})

	query := bson.M{
		"garbage": bson.M{"$ne": true},
	}

	if rev == "latest" {
		query["device"] = "prn:::devices:/" + trailID
	} else if rev != "" {
		query["_id"] = fmt.Sprintf("%s-%s", trailID, rev)
	} else {
		query["_id"] = trailID
	}

	err := coll.FindOne(ctxi, query, findOneOptions).Decode(&step)
	if err != nil {
		rerr = &utils.RError{
			Msg:   fmt.Sprintf("step not found %++v", query),
			Error: err.Error(),
			Code:  http.StatusNotFound,
		}
		return step, rerr
	}

	if step == nil {
		rerr = &utils.RError{
			Msg:   "no step found for trail: " + trailID,
			Error: "no step found for trail: " + trailID,
			Code:  http.StatusNotFound,
		}
		return step, rerr
	}

	return step, rerr
}

func (s *TService) GetTrailObjectsWithAccess(
	ctx context.Context,
	deviceID,
	rev,
	owner,
	authType string,
	isPublic bool,
	frags string,
) (owa []objects.ObjectWithAccess, rerr *utils.RError) {
	coll := s.db.Collection("pantahub_steps")
	ctxi, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var err error
	step := &trailmodels.Step{}

	trailID := deviceID + "-" + rev
	if isPublic {
		err = coll.FindOne(ctxi, bson.M{
			"_id":     trailID,
			"garbage": bson.M{"$ne": true},
		}).Decode(step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctxi, bson.M{
			"_id":     trailID,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(step)
	} else if authType == "USER" || authType == "SESSION" {
		err = coll.FindOne(ctxi, bson.M{
			"_id":     trailID,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(step)
	}
	if err != nil {
		rerr = &utils.RError{
			Error: err.Error(),
			Msg:   "No trail found",
			Code:  http.StatusNotFound,
		}
		return owa, rerr
	}

	owa = make([]objects.ObjectWithAccess, 0)
	stepState := utils.BsonUnquoteMap(&step.State)
	stateU := stepState
	if frags != "" {
		stateU, err = libpvr.FilterByFrags(stepState, frags)
		if err != nil {
			rerr = &utils.RError{
				Error: err.Error(),
				Msg:   "",
				Code:  http.StatusInternalServerError,
			}
			return owa, rerr
		}
	}

	collection := s.db.Collection("pantahub_objects")
	if collection == nil {
		rerr = &utils.RError{
			Error: "Error with Database connectivity",
			Msg:   "Error with Database connectivity",
			Code:  http.StatusInternalServerError,
		}
		return owa, rerr
	}

	for k, v := range stateU {
		_, ok := v.(string)

		if !ok {
			// we found a json element
			continue
		}

		if k == "#spec" {
			continue
		}

		objID := v.(string)
		sha, err := utils.DecodeSha256HexString(objID)

		if err != nil {
			rerr = &utils.RError{
				Error: err.Error(),
				Msg:   "Get Steps Object id must be a valid sha256",
				Code:  http.StatusBadRequest,
			}
			return owa, rerr
		}

		storageID := objects.MakeStorageID(step.Owner, sha)
		var newObject objects.Object

		ctxi, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		err = collection.
			FindOne(ctxi, bson.M{"_id": storageID, "garbage": bson.M{"$ne": true}}).
			Decode(&newObject)

		if err != nil {
			rerr = &utils.RError{
				Error: err.Error(),
				Msg:   "Not Accessible Resource Id: " + storageID,
				Code:  http.StatusForbidden,
			}
			return owa, rerr
		}

		if newObject.Owner != step.Owner {
			rerr = &utils.RError{
				Error: "Invalid Object Access",
				Msg:   "Invalid Object Access",
				Code:  http.StatusForbidden,
			}
			return owa, rerr
		}

		newObject.ObjectName = k

		issuerURL := utils.GetAPIEndpoint("/trails")
		objWithAccess := objects.MakeObjAccessible(issuerURL, owner, newObject, storageID)
		owa = append(owa, objWithAccess)
	}

	return owa, rerr
}
