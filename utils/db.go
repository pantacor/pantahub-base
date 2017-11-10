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
	"log"

	"gopkg.in/mgo.v2"
)

func GetMongoSession() (*mgo.Session, error) {
	// XXX: make mongo host configurable through env
	mongoDb := GetEnv(ENV_MONGO_DB)
	mongoHost := GetEnv(ENV_MONGO_HOST)
	mongoPort := GetEnv(ENV_MONGO_PORT)
	mongoUser := GetEnv(ENV_MONGO_USER)
	mongoPass := GetEnv(ENV_MONGO_PASS)
	mongoRs := GetEnv(ENV_MONGO_RS)

	mongoCreds := ""
	if mongoUser != "" {
		mongoCreds = mongoUser + ":" + mongoPass + "@"
	}

	mongoConnect := "mongodb://" + mongoCreds + mongoHost + ":" + mongoPort + "/" + mongoDb

	if mongoRs != "" {
		mongoConnect = mongoConnect + "?replicaSet=" + mongoRs
	}
	log.Println("Will connect to mongodb with: " + mongoConnect)

	return mgo.Dial(mongoConnect)
}
