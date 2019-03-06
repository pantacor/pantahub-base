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
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo/options"

	"go.mongodb.org/mongo-driver/mongo"
)

// MongoDb : Holds Mongo Db Name
var MongoDb string

// GetMongoClient : To Get Mongo Client Object
func GetMongoClient() (*mongo.Client, error) {
	MongoDb = GetEnv(ENV_MONGO_DB)
	host := GetEnv(ENV_MONGO_HOST)
	port := GetEnv(ENV_MONGO_PORT)
	mongoUser := GetEnv(ENV_MONGO_USER)
	mongoPass := GetEnv(ENV_MONGO_PASS)
	mongoRs := GetEnv(ENV_MONGO_RS)

	//Setting Client Options
	clientOptions := options.Client()
	credentials := options.Credential{
		//AuthMechanism: "SCRAM-SHA-1",
		//AuthSource:  MongoDb,
		PasswordSet: false,
	}
	clientOptions.SetAuth(credentials)
	//mongoRs = "rs0"
	clientOptions.SetReplicaSet(mongoRs)
	mongoConnect := "mongodb://" + mongoUser + ":" + mongoPass + "@" + host + ":" + port + "/" + MongoDb
	//mongoConnect := "mongodb://" + host + ":" + port + "/" + MongoDb
	//mongoConnect := host + ":" + port
	//mongoConnect := "http://127.0.0.1:27017"
	/*hosts := []string{
		mongoConnect,
	} */
	//clientOptions.SetHosts(hosts)
	clientOptions.ApplyURI(mongoConnect)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))

	/*
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		client, err := mongo.Connect(ctx, clientOptions)
		if err != nil {
			panic(err)
		}
	*/
	log.Println("Will connect to mongo PROD db with: " + mongoConnect)

	return client, err
}

// GetMongoClienttest : To Get Mongo Client Object of test db
func GetMongoClientTest() (*mongo.Client, error) {
	MongoDb = "testdb-" + GetEnv(ENV_MONGO_DB)
	host := GetEnv(ENV_MONGO_HOST)
	port := GetEnv(ENV_MONGO_PORT)
	mongoUser := GetEnv(ENV_MONGO_USER)
	mongoPass := GetEnv(ENV_MONGO_PASS)
	mongoRs := GetEnv(ENV_MONGO_RS)

	//Setting Client Options
	clientOptions := options.Client()
	credentials := options.Credential{
		Username: mongoUser,
		Password: mongoPass,
	}
	//mongoConnect := "mongodb://" + mongoUser + ":" + mongoPass + "@" + host + ":" + port + "/" + MongoDb
	mongoConnect := "mongodb://" + host + ":" + port + "/" + MongoDb
	hosts := []string{
		mongoConnect,
	}
	clientOptions.SetHosts(hosts)
	clientOptions.SetAuth(credentials)
	clientOptions.SetReplicaSet(mongoRs)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		panic(err)
	}
	log.Println("Will connect to mongo PROD db with: " + mongoConnect)
	return client, err
}
