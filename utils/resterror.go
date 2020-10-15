package utils

import (
	"fmt"
	"log"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
)

// UserError user error type
type UserError struct {
	Msg string
}

// RError rest error struct
type RError struct {
	Error string `json:"error"`
	Msg   string `json:"msg,omitempty"`
	Code  int    `json:"cod,omitemptye"`
}

func (userError *UserError) Error() string {
	return userError.Msg
}

// IsUserError check if an error is the type UserError
func IsUserError(err error) bool {
	_, ok := err.(*UserError)
	return ok
}

// UserErrorNew user error factory
func UserErrorNew(msg string) *UserError {
	return &UserError{Msg: msg}
}

// RestErrorUser Create a rest error with id and log
func RestErrorUser(w rest.ResponseWriter, err error, message string, statusCode int) {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	RestErrorWrapperUser(w, errStr, message, statusCode)
}

// RestError Create a rest error with id and log
func RestError(w rest.ResponseWriter, err error, message string, statusCode int) {
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	RestErrorWrapper(w, message+" "+errStr, statusCode)
}

func restErrorWrapperInternal(w rest.ResponseWriter, errorStr, userMsg string, code int) {
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

// RestErrorWrapperUser wrap the normal rest error in an struct
func RestErrorWrapperUser(w rest.ResponseWriter, errorStr, userMessage string, code int) {
	restErrorWrapperInternal(w, errorStr, userMessage, code)
}

// RestErrorWrapper wrap the normal rest error in an struct
func RestErrorWrapper(w rest.ResponseWriter, errorStr string, code int) {
	restErrorWrapperInternal(w, errorStr, "", code)
}
