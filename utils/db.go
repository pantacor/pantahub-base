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
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
	"gopkg.in/mgo.v2"

	"go.mongodb.org/mongo-driver/mongo"
)

// MongoDb : Holds Mongo Db Name
var MongoDb string

// GetMongoClient : To Get Mongo Client Object
func GetMongoClient() (*mongo.Client, error) {
	MongoDb = GetEnv(EnvMongoDb)
	user := GetEnv(EnvMongoUser)
	pass := GetEnv(EnvMongoPassword)
	host := GetEnv(EnvMongoHost)
	port := GetEnv(EnvMongoPort)
	mongoRs := GetEnv(EnvMongoRs)
	ssl := GetEnv(EnvMongoSsl)

	//Setting Client Options
	clientOptions := options.Client()
	mongoConnect := "mongodb://"
	if user != "" {
		mongoConnect += user
		if pass != "" {
			mongoConnect += ":"
			mongoConnect += pass
		}
		mongoConnect += "@"
	}
	mongoConnect += host

	if port != "" {
		mongoConnect += ":"
		mongoConnect += port
	}

	mongoConnect += "/?"

	if user != "" {
		mongoConnect += "authSource=" + MongoDb
		mongoConnect += "&authMechanism=SCRAM-SHA-1"
	}

	if mongoRs != "" {
		mongoConnect += "&replicaSet=" + mongoRs
	}

	if ssl == "false" {
		mongoConnect += "&ssl=false"

	}

	clientOptions = clientOptions.ApplyURI(mongoConnect)
	if mongoRs != "" {
		clientOptions = clientOptions.SetReplicaSet(mongoRs)
	}

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		clientOptions.SetMonitor(otelmongo.NewMonitor())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Println("Will connect to mongo PROD db with: " + mongoConnect)
	client, err := mongo.Connect(ctx, clientOptions)

	return client, err
}

// GetMongoClientTest : To Get Mongo Client Object
func GetMongoClientTest() (*mongo.Client, error) {
	MongoDb = GetEnv(EnvMongoDb)
	MongoDb = "testdb-" + MongoDb
	user := GetEnv(EnvMongoUser)
	pass := GetEnv(EnvMongoPassword)
	host := GetEnv(EnvMongoHost)
	port := GetEnv(EnvMongoPort)
	mongoRs := GetEnv(EnvMongoRs)

	//Setting Client Options
	clientOptions := options.Client()
	mongoConnect := "mongodb://"
	if user != "" {
		mongoConnect += user
		if pass != "" {
			mongoConnect += ":"
			mongoConnect += pass
		}
		mongoConnect += "@"
	}
	mongoConnect += host

	if port != "" {
		mongoConnect += ":"
		mongoConnect += port
	}

	mongoConnect += "/?"

	clientOptions = clientOptions.ApplyURI(mongoConnect)
	if mongoRs != "" {
		clientOptions = clientOptions.SetReplicaSet(mongoRs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Println("Will connect to mongo PROD db with: " + mongoConnect)
	client, err := mongo.Connect(ctx, clientOptions)

	return client, err
}

// GetMongoSession is the legacy util to access database through old mgo driver
func GetMongoSession() (*mgo.Session, error) {
	// XXX: make mongo host configurable through env
	mongoDb := GetEnv(EnvMongoDb)
	mongoHost := GetEnv(EnvMongoHost)
	mongoPort := GetEnv(EnvMongoPort)
	mongoUser := GetEnv(EnvMongoUser)
	mongoPass := GetEnv(EnvMongoPassword)
	mongoRs := GetEnv(EnvMongoRs)

	mongoCreds := ""
	if mongoUser != "" {
		mongoCreds = mongoUser + ":" + mongoPass + "@"
	}

	mongoConnect := "mongodb://" + mongoCreds + mongoHost + ":" + mongoPort + "/" + mongoDb

	if mongoRs != "" {
		mongoConnect = mongoConnect + "?replicaSet=" + mongoRs
	}
	log.Println("Will connect to mongo PROD db with: " + mongoConnect)

	return mgo.Dial(mongoConnect)
}

// GetMongoSessionTest get a test session of mongo
func GetMongoSessionTest() (*mgo.Session, error) {
	// XXX: make mongo host configurable through env
	mongoDb := "testdb-" + GetEnv(EnvMongoDb)
	mongoHost := GetEnv(EnvMongoHost)
	mongoPort := GetEnv(EnvMongoPort)
	mongoUser := GetEnv(EnvMongoUser)
	mongoPass := GetEnv(EnvMongoPassword)
	mongoRs := GetEnv(EnvMongoRs)

	mongoCreds := ""
	if mongoUser != "" {
		mongoCreds = mongoUser + ":" + mongoPass + "@"
	}

	mongoConnect := "mongodb://" + mongoCreds + mongoHost + ":" + mongoPort + "/" + mongoDb

	if mongoRs != "" {
		mongoConnect = mongoConnect + "?replicaSet=" + mongoRs
	}
	log.Println("Will connect to mongo TEST db with: " + mongoConnect)

	return mgo.Dial(mongoConnect)
}
