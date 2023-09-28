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

package devices

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"

	"gopkg.in/mgo.v2/bson"
)

type disableToken struct {
	Status string `json:"status"`
}

// helper function to make it easy to get info based on auth auth token...
func (a *App) getBase64AutoTokenInfo(ctx context.Context, tokenBase64 string) (*autoTokenInfo, error) {

	tok := make([]byte, 24)

	_, err := base64.StdEncoding.Decode(tok, []byte(tokenBase64))
	if err != nil {
		return nil, err
	}

	shaSummer := sha256.New()
	_, err = shaSummer.Write(tok)

	sum := make([]byte, shaSummer.Size())
	tokenSha := shaSummer.Sum(sum)

	res := utils.PantahubDevicesJoinToken{}

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = col.FindOne(ctxC, bson.M{"tokensha": tokenSha}).Decode(&res)
	if err != nil {
		return nil, errors.New("token not found")
	}

	if res.Disabled {
		return nil, errors.New("token disabled")
	}

	result := autoTokenInfo{}
	result.Owner = res.Owner
	result.UserMeta = utils.BsonQuoteMap(&res.DefaultUserMeta)

	return &result, nil
}

// EnsureTokenIndices create devices database indices
func (a *App) EnsureTokenIndices() error {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "disabled", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "tokensha", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices_tokens: " + err.Error())
		return nil
	}

	return nil
}
