package utils

import (
	"fmt"
	"os"

	"gopkg.in/mgo.v2"
)

func GetMongoSession() (*mgo.Session, error) {
	// XXX: make mongo host configurable through env
	mongoHost := os.Getenv("MONGO_HOST")
	if mongoHost == "" {
		mongoHost = "localhost"
	}

	mongoPort := os.Getenv("MONGO_PORT")
	if mongoPort == "" {
		mongoPort = "27017"
	}

	mongoUser := os.Getenv("MONGO_USER")
	mongoPass := os.Getenv("MONGO_PASS")

	mongoCreds := ""
	if mongoUser != "" {
		mongoCreds += mongoUser + ":" + mongoPass + "@"
	}

	mongoDb := os.Getenv("MONGO_DB")
	if mongoDb == "" {
		mongoDb = "pantahub-base"
	}

	mongoConnect := "mongodb://" + mongoCreds + mongoHost + ":" + mongoPort + "/" + mongoDb
	fmt.Println("mongodb connect: " + mongoConnect)

	return mgo.Dial(mongoConnect)
}
