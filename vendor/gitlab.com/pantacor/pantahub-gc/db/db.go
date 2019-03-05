//
// Copyright 2018  Pantacor Ltd.
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

package db

import (
	"context"
	"time"

	"github.com/mongodb/mongo-go-driver/mongo"
	"gitlab.com/pantacor/pantahub-base/utils"
	mgo "gopkg.in/mgo.v2"
)

// Session : MongoDb object
var Session *mgo.Database

// MongoDb : MongoDb Object
var MongoDb *mongo.Database

// Connect : To connect to the mongoDb
func Connect() (*mgo.Database, *mongo.Database) {
	mongoDatabase := utils.GetEnv("MONGO_DB")

	session, err := utils.GetMongoSession()
	if err != nil {
		panic(err)
	}
	//defer session.Close()
	Session = session.DB(mongoDatabase)
	//mongo-go-driver settings
	host := utils.GetEnv("MONGO_HOST")
	port := utils.GetEnv("MONGO_PORT")
	ctx, cancelFunction := context.WithTimeout(context.Background(), 10*time.Second)
	cancelFunction()
	client, err := mongo.Connect(ctx, "mongodb://"+host+":"+port)
	if err != nil {
		panic(err)
	}
	MongoDb = client.Database(mongoDatabase)
	return Session, MongoDb
}
