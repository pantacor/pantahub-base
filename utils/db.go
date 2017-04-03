//
// Copyright 2017  Alexander Sack <asac129@gmail.com>
//
package utils

import (
	"fmt"

	"gopkg.in/mgo.v2"
)

func GetMongoSession() (*mgo.Session, error) {
	// XXX: make mongo host configurable through env
	mongoDb := GetEnv(ENV_MONGO_DB)
	mongoHost := GetEnv(ENV_MONGO_HOST)
	mongoPort := GetEnv(ENV_MONGO_PORT)
	mongoUser := GetEnv(ENV_MONGO_USER)
	mongoPass := GetEnv(ENV_MONGO_PASS)

	mongoCreds := ""
	if mongoUser != "" {
		mongoCreds = mongoUser + ":" + mongoPass + "@"
	}

	mongoConnect := "mongodb://" + mongoCreds + mongoHost + ":" + mongoPort + "/" + mongoDb
	fmt.Println("mongodb connect: " + mongoConnect)

	return mgo.Dial(mongoConnect)
}
