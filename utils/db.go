//
// Copyright 2017  Pantacor Ltd.
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
package utils

import (
	"context"
	"time"

	"github.com/mongodb/mongo-go-driver/mongo"
)

// MongoDb : Holds Mongo Db Name
var MongoDb string

// GetMongoClient : To Get Mongo Client Object
func GetMongoClient() (*mongo.Client, error) {
	MongoDb = GetEnv(ENV_MONGO_DB)
	host := GetEnv(ENV_MONGO_HOST)
	port := GetEnv(ENV_MONGO_PORT)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, "mongodb://"+host+":"+port)
	if err != nil {
		panic(err)
	}
	return client, err
}
