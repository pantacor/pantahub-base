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

package objects

import (
	"context"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// Object define a object structure
type Object struct {
	ID           string `json:"id" bson:"id"`
	StorageID    string `json:"storage-id" bson:"_id"`
	Owner        string `json:"owner"`
	ObjectName   string `json:"objectname"`
	Sha          string `json:"sha256sum"`
	Size         string `json:"size"`
	SizeInt      int64  `json:"sizeint"`
	MimeType     string `json:"mime-type"`
	initialized  bool
	LinkedObject string `json:"-" bson:"linked_object"`
}

// ObjectWithAccess extends object to add access information
type ObjectWithAccess struct {
	Object       `bson:",inline"`
	SignedPutURL string `json:"signed-puturl"`
	SignedGetURL string `json:"signed-geturl"`
	Now          string `json:"now"`
	ExpireTime   string `json:"expire-time"`
}

// DiskQuotaUsageResult payload for disk quota usage
type DiskQuotaUsageResult struct {
	ID    string  `json:"id" bson:"_id"`
	Total float64 `json:"total"`
}

// CalcUsageAfterPost calculate usage after post new object
func CalcUsageAfterPost(owner string, mongoClient *mongo.Client,
	objectID string, newSize int64) (*DiskQuotaUsageResult, error) {

	oCol := mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	resp := DiskQuotaUsageResult{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipeline := []bson.M{
		bson.M{
			"$match": bson.M{
				"owner":   owner,
				"garbage": bson.M{"$ne": true},
			},
		},
		bson.M{
			"$group": bson.M{
				"_id":   "$owner",
				"total": bson.M{"$sum": "$sizeint"},
			},
		},
	}
	cur, err := oCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := DiskQuotaUsageResult{}
		err := cur.Decode(&result)
		if err != nil {
			return nil, err
		}
		result.Total += float64(newSize)
		resp = result
		break
	}
	return &resp, nil
}

// CalcUsageAfterPut calculate disk usage after update object
func CalcUsageAfterPut(owner string, mongoClient *mongo.Client,
	objectID string, newSize int64) (*DiskQuotaUsageResult, error) {

	oCol := mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	resp := DiskQuotaUsageResult{}
	// match all objects, but leave out the one we replace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipeline := []bson.M{
		bson.M{
			"$match": bson.M{
				"owner":   owner,
				"garbage": bson.M{"$ne": true},
				"_id":     bson.M{"$ne": objectID},
			},
		},
		bson.M{
			"$group": bson.M{
				"_id":   "$owner",
				"total": bson.M{"$sum": "$sizeint"},
			},
		},
	}
	cur, err := oCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := DiskQuotaUsageResult{}
		err := cur.Decode(&result)
		if err != nil {
			return nil, err
		}
		result.Total += float64(newSize)
		resp = result
		break
	}
	return &resp, nil
}

// FindObjectByStorageID find object instance by _id
func (a *App) FindObjectByStorageID(storageID string, object *Object) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := collection.FindOne(ctx,
		bson.M{
			"_id": storageID,
		},
	).Decode(object)
	if err != nil {
		return err
	}
	return nil
}

/*
func (o *Object) FindById(ctx context.Context, objectId string) {
	ctx.Value(OBJECTS_ACCESS_PRINCIPAL)
}

func (o *Object) Reload(ctx context.Context) {
}

type Page struct {
	Start  int
	Size   int
	Len    int
	Length int
	Data   []interface{}
}

type Objects []Object

func (o *Objects) FindColl(objectId string, filter map[string]interface{}) {
}

func (o *Objects) FindColl(objectId string, filter map[string]interface{}, start, size int) {
}
*/
