//
// Copyright 2017, 2018  Pantacor Ltd.
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

package logs

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
	"gopkg.in/mgo.v2/bson"
)

type mgoLogger struct {
	mongoClient   *mongo.Client
	mgoCollection string
}

func (s *mgoLogger) register() error {
	var err error
	collection := s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "own", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}

	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for " + s.mgoCollection + ": " + err.Error())
		return nil
	}

	collection = s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "dev", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}

	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for " + s.mgoCollection + ": " + err.Error())
		return nil
	}

	collection = s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "time-created", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}

	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for " + s.mgoCollection + ": " + err.Error())
		return nil
	}

	collection = s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "tsec", Value: bsonx.Int32(1)},
			{Key: "tnano", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}

	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for " + s.mgoCollection + ": " + err.Error())
		return nil
	}

	collection = s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "lvl", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}

	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for " + s.mgoCollection + ": " + err.Error())
		return nil
	}

	collection = s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "dev", Value: bsonx.Int32(1)},
			{Key: "own", Value: bsonx.Int32(1)},
			{Key: "time-created", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}

	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for " + s.mgoCollection + ": " + err.Error())
		return nil
	}

	return nil
}

func (s *mgoLogger) unregister(delete bool) error {
	if delete {
		err := s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection).Drop(nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *mgoLogger) getLogs(parentCtx context.Context, start int64, page int64, before *time.Time,
	after *time.Time, query Filters, sort Sorts, cursor bool) (*Pager, error) {
	var result Pager
	var err error

	if cursor {
		return nil, ErrCursorNotImplemented
	}

	sortStr := strings.Join(sort, ",")
	collLogs := s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)

	if collLogs == nil {
		return nil, errors.New("Couldnt instantiate mgo connection for collection " + s.mgoCollection)
	}

	findFilter := bson.M{}

	if query.Owner != "" {
		findFilter["own"] = query.Owner
	}

	if query.Device != "" {
		findFilter["dev"] = bson.M{
			"$in": strings.Split(query.Device, ","),
		}
	}
	if query.LogRev != "" {
		findFilter["rev"] = bson.M{
			"$in": strings.Split(query.LogRev, ","),
		}
	}
	if query.LogPlat != "" {
		findFilter["plat"] = bson.M{
			"$in": strings.Split(query.LogPlat, ","),
		}
	}
	if query.LogSource != "" {
		findFilter["src"] = bson.M{
			"$in": strings.Split(query.LogSource, ","),
		}
	}
	if query.LogLevel != "" {
		findFilter["lvl"] = bson.M{
			"$in": strings.Split(query.LogLevel, ","),
		}
	}

	if before != nil {
		findFilter["time-created"] = bson.M{
			"$lt": before,
		}
	}
	if after != nil {
		findFilter["time-created"] = bson.M{
			"$gt": after,
		}
	}

	// default sort by reverse time
	if sortStr == "" {
		sortStr =
			"-time-created"
	}

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	if start > 0 {
		findOptions.SetSkip(start)
	}
	if page > 0 {
		findOptions.SetLimit(page)
	}

	sortFields := bson.M{}
	for _, v := range sort {
		if v[0:0] == "-" {
			sortFields[v] = -1
		} else {
			sortFields[v] = 1
		}
	}
	if len(sortFields) > 0 {
		findOptions.SetSort(sortFields)
	} else {
		findOptions.SetSort(bson.M{"time-created": -1})
	}
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	cur, err := collLogs.Find(ctx, findFilter, findOptions)
	if err != nil {
		return nil, err
	}

	defer cur.Close(ctx)
	entries := []*Entry{}

	for cur.Next(ctx) {
		result := &Entry{}
		err := cur.Decode(&result)
		if err != nil {
			return nil, err
		}
		entries = append(entries, result)
	}
	ctx, cancel = context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	count, err := collLogs.CountDocuments(ctx, findFilter)
	if err != nil {
		return nil, err
	}
	result.Count = count
	result.Start = start
	result.Page = page

	result.Entries = entries
	return &result, nil
}

func (s *mgoLogger) getLogsByCursor(parentCtx context.Context, nextCursor string) (*Pager, error) {
	return nil, ErrCursorNotImplemented
}

func (s *mgoLogger) postLogs(parentCtx context.Context, e []Entry) error {
	collLogs := s.mongoClient.Database(utils.MongoDb).Collection(s.mgoCollection)

	if collLogs == nil {
		return errors.New("Error with Database connectivity")
	}

	arr := make([]interface{}, len(e))
	for i, v := range e {
		arr[i] = v
	}
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	_, err := collLogs.InsertMany(
		ctx,
		arr,
	)
	if err != nil {
		return err
	}

	return nil
}

// NewMgoLogger instantiates an mongoClient logger backend. Expects an
// mongoClient configuration
func NewMgoLogger(mongoClient *mongo.Client) (Backend, error) {
	return newMgoLogger(mongoClient)
}

func newMgoLogger(mongoClient *mongo.Client) (*mgoLogger, error) {
	self := &mgoLogger{}
	self.mgoCollection = utils.GetEnv(utils.EnvPantahubProductName) + "_logs"
	self.mongoClient = mongoClient

	return self, nil
}
