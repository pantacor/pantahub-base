package utils

import (
	"fmt"
	"log"

	"github.com/ant0ine/go-json-rest/rest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func RestError(w rest.ResponseWriter, err error, message string, status_code int) {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	errId := primitive.NewObjectID()
	log.Println("ERROR: " + message + " -- " + errStr + " -- statuscode: " + fmt.Sprintf("%d", status_code) + " -- sid: " + errId.Hex())
	rest.Error(w, message+" (sid: "+errId.Hex()+")", status_code)
}
