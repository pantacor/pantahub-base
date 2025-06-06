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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/gosimple/slug"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// CreateOrUpdateApp a new third party app
func CreateOrUpdateApp(ctx context.Context, tpApp *TPApp, database *mongo.Database) (*TPApp, error) {
	if tpApp.Nick == "" {
		tpApp.Nick = petname.Generate(3, "-")
	} else {
		tpApp.Nick = slug.Make(tpApp.Nick)
	}

	if tpApp.Type != AppTypeConfidential {
		tpApp.ExposedScopes = []utils.Scope{}
		tpApp.Secret = ""
	}

	collection := database.Collection(DBCollection)
	if collection == nil {
		return nil, errors.New("error with Database connectivity")
	}

	tpApp.ExposedScopesLength = len(tpApp.ExposedScopes)

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := collection.UpdateOne(
		ctxC,
		bson.M{"_id": tpApp.ID},
		bson.M{"$set": tpApp},
		updateOptions,
	)

	return tpApp, err
}

// LoginAsApp using and application id and secret
func LoginAsApp(serviceID, secret string, database *mongo.Database) (*TPApp, error) {
	collection := database.Collection(DBCollection)
	if collection == nil {
		return nil, errors.New("error with Database connectivity")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	findQuery := bson.M{
		"secret":     secret,
		"deleted-at": nil,
	}

	if serviceID != "" {
		ObjectID, _ := primitive.ObjectIDFromHex(serviceID)
		findQuery["$or"] = []bson.M{
			{"_id": ObjectID},
			{"prn": serviceID},
		}
	}

	tpApp := &TPApp{}
	dbResult := collection.FindOne(ctx, findQuery)

	dbResult.Decode(tpApp)
	return tpApp, nil
}

// SearchApp search third party app by id or prn
func SearchApp(ctx context.Context, owner string, id string, database *mongo.Database) (*TPApp, int, error) {
	apps, err := SearchApps(ctx, owner, id, database)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("error reading third party application " + err.Error())
	}

	if len(apps) != 1 {
		return nil, http.StatusNotFound, errors.New("App not found (id " + id + ")")
	}

	tpApp := apps[0]

	return &tpApp, 0, nil
}

// SearchApps search all third party app by id or prn
func SearchApps(ctx context.Context, owner string, id string, database *mongo.Database) ([]TPApp, error) {
	apps := make([]TPApp, 0)

	collection := database.Collection(DBCollection)
	if collection == nil {
		return apps, errors.New("error with Database connectivity")
	}

	findQuery := bson.M{
		"deleted-at": nil,
	}

	if owner != "" {
		findQuery["owner"] = owner
	}

	if id != "" {
		ObjectID, _ := primitive.ObjectIDFromHex(id)
		findQuery["$or"] = []bson.M{
			{"_id": ObjectID},
			{"prn": id},
			{"nick": id},
		}
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cur, err := collection.Find(ctxC, findQuery)
	if err != nil {
		return apps, err
	}
	defer cur.Close(ctxC)

	for cur.Next(ctxC) {
		result := TPApp{}
		err := cur.Decode(&result)
		if err != nil {
			return apps, err
		}
		apps = append(apps, result)
	}

	return apps, nil
}

// SearchExposedScopes search all third party app by id or prn
func SearchExposedScopes(ctx context.Context, database *mongo.Database) ([]utils.Scope, error) {
	scopes := make([]utils.Scope, 0)

	collection := database.Collection(DBCollection)
	if collection == nil {
		return nil, errors.New("error with Database connectivity")
	}

	findQuery := bson.M{
		"deleted-at": nil,
		"exposed_scopes_length": bson.M{
			"$gt": 0,
		},
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cur, err := collection.Find(ctxC, findQuery)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctxC)

	for cur.Next(ctxC) {
		result := TPApp{}
		err := cur.Decode(&result)
		if err != nil {
			return scopes, err
		}
		scopes = append(scopes, result.ExposedScopes...)
	}

	return scopes, nil
}

// AccessCodePayload get accesscode payload for application
func AccessCodePayload(ctx context.Context, owner, serviceName, responseType, scopes string, accountPayload map[string]interface{}, database *mongo.Database) (map[string]interface{}, error) {
	service, _, err := SearchApp(ctx, owner, serviceName, database)
	if err != nil {
		return nil, err
	}

	if responseType == "code" && service.Type != AppTypeConfidential {
		return nil, errors.New("Application can respond with code because the application is public")
	}

	result := map[string]interface{}{}
	result["approver_prn"] = accountPayload["prn"]
	result["approver_nick"] = accountPayload["nick"]
	result["approver_roles"] = accountPayload["roles"]
	result["approver_type"] = accountPayload["type"]
	result["service"] = service.Prn
	result["scopes"] = scopes

	return result, nil
}

// GetAppPayload get app payload as account
func GetAppPayload(ctx context.Context, serviceID string, database *mongo.Database) (map[string]interface{}, error) {
	service, _, err := SearchApp(ctx, "", serviceID, database)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{}
	result["roles"] = "service"
	result["type"] = "SERVICE"
	result["id"] = service.Prn
	result["nick"] = service.Nick
	result["prn"] = service.Prn
	result["scopes"] = strings.Join(utils.MarshalScopes(service.Scopes), ",")

	return result, nil
}
