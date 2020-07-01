package utils

import (
	"fmt"
	"log"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// RError rest error struct
type RError struct {
	Error string `json:"error"`
	Msg   string `json:"msg",omitempty`
	Code  int    `json:"code,omitempty"`
}

// RestError Create a rest error with id and log
func RestError(w rest.ResponseWriter, err error, message string, statusCode int) {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	errID := primitive.NewObjectID()
	log.Println("ERROR: " + message + " -- " + errStr + " -- statuscode: " + fmt.Sprintf("%d", statusCode) + " -- sid: " + errID.Hex())
	RestErrorWrapper(w, message+" (sid: "+errID.Hex()+")", statusCode)
}

func restErrorWrapperInternal(w rest.ResponseWriter, errorStr string, userMsg string, code int) {
	incidentID := time.Now().UnixNano()

	incidentStr := fmt.Sprintf("REST-ERR-ID-%d", incidentID)
	log.Printf("ERROR| %s: %s", incidentStr, errorStr)

	w.WriteHeader(code)
	err := w.WriteJson(RError{
		Error: incidentStr,
		Msg:   userMsg,
		Code:  code,
	})
	if err != nil {
		panic(err)
	}
}

// RestErrorWrapper wrap the normal rest error in an struct
func RestErrorWrapperUser(w rest.ResponseWriter, errorStr string, code int) {
	restErrorWrapperInternal(w, errorStr, errorStr, code)
}

// RestErrorWrapper wrap the normal rest error in an struct
func RestErrorWrapper(w rest.ResponseWriter, errorStr string, code int) {
	restErrorWrapperInternal(w, errorStr, "", code)
}
