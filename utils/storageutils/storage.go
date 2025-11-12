// Copyright 2025  Pantacor Ltd.
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

package storageutils

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
	"go.opentelemetry.io/otel"
)

var (
	storage Storage
)

// Storage define all storage action and methods
type Storage struct {
	Database         string
	CollectionPrefix string
	client           *mongo.Client
	timeoutDuration  time.Duration
}

// IsNotFound resource not found
func IsNotFound(err error) bool {
	return err == mongo.ErrNoDocuments
}

// IsKeyDuplicated test if a key already exist on storage
func IsKeyDuplicated(err error) bool {
	return strings.Contains(err.Error(), "duplicate key error collection")
}

// IsDuplicateKey test if a key already exist on storage
func IsDuplicateKey(key string, err error) bool {
	return strings.Contains(err.Error(), "duplicate key error collection") &&
		strings.Contains(err.Error(), "index: "+key)

}

func (s *Storage) GetDatabase() *mongo.Database {
	return s.client.Database(s.Database)
}

func (s *Storage) GetCollection(name string) *mongo.Collection {
	name = s.CollectionPrefix + name
	return s.client.Database(s.Database).Collection(name)
}

// New create new Storage Struct
func New(prefix string) (*Storage, error) {
	if storage.client != nil {
		return &storage, nil
	}

	client, err := GetMongoClient()
	if err != nil {
		return nil, err
	}

	timeout, err := time.ParseDuration(utils.GetEnvDefault("MONGO_TIMEOUT_DURATION", "30m"))
	if err != nil {
		return nil, err
	}

	mongoDb := utils.GetEnvDefault("MONGO_DB", "")
	storage = Storage{
		client:           client,
		Database:         mongoDb,
		timeoutDuration:  timeout,
		CollectionPrefix: prefix,
	}
	return &storage, nil
}

// GetMongoClient : To Get Mongo Client Object
func GetMongoClient() (*mongo.Client, error) {
	mongoDb := utils.GetEnvDefault("MONGO_DB", "")
	user := utils.GetEnvDefault("MONGO_USER", "")
	pass := utils.GetEnvDefault("MONGO_PASS", "")
	host := utils.GetEnvDefault("MONGO_HOST", "localhost")
	port := utils.GetEnvDefault("MONGO_PORT", "27017")
	mongoRs := utils.GetEnvDefault("MONGO_RS", "")

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

	mongoConnect += "/" + mongoDb + "?"

	if user != "" {
		mongoConnect += "authSource=" + mongoDb
		mongoConnect += "&authMechanism=SCRAM-SHA-1"
	}

	if mongoRs != "" {
		mongoConnect += "&replicaSet=" + mongoRs
	}

	clientOptions = clientOptions.ApplyURI(mongoConnect)
	if mongoRs != "" {
		clientOptions = clientOptions.SetReplicaSet(mongoRs)
	}
	clientOptions.SetMaxPoolSize(6)
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		clientOptions.SetMonitor(otelmongo.NewMonitor(
			otelmongo.WithCommandAttributeDisabled(false),
			otelmongo.WithTracerProvider(otel.GetTracerProvider()),
		))
	}

	timeoutEnv := utils.GetEnvDefault("MONGO_TIMEOUT_DURATION", "30m")
	timeout, err := time.ParseDuration(timeoutEnv)
	if err != nil {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Println("Will connect to mongodb with: " + mongoConnect)
	client, err := mongo.Connect(ctx, clientOptions)
	return client, err
}
