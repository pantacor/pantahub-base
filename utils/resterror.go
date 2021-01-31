package utils

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/fatih/structs"
	"github.com/fluent/fluent-logger-golang/fluent"
)

var logger *fluent.Fluent

// UserError user error type
type UserError struct {
	Msg string
}

// RError rest error struct
type RError struct {
	IncidentID *int64 `json:"incident,omitempty"`
	Error      string `json:"error"`
	Msg        string `json:"msg,omitempty"`
	Code       int    `json:"cod,omitemptye"`
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

func getLogger() *fluent.Fluent {
	if logger == nil {
		portStr := GetEnv(EnvFluentPort)
		port, err := strconv.Atoi(portStr)
		if err != nil {
			log.Fatalln("FATAL: cannot read fluent logger settings: " + err.Error())
		}
		host := GetEnv(EnvFluentHost)
		logger, err = fluent.New(fluent.Config{FluentPort: port, FluentHost: host})
		if err != nil {
			log.Fatalln("FATAL: cannot initialize fluent logger: " + err.Error())
		}
	}

	return logger
}

func restErrorWrapperInternal(w rest.ResponseWriter, errorStr, userMsg string, code int) {
	incidentID := time.Now().UnixNano()

	incidentStr := fmt.Sprintf("REST-ERR-ID-%d", incidentID)
	incidentDetails := fmt.Sprintf("ERROR| %s: %s", incidentStr, errorStr)
	log.Printf(incidentDetails)

	rError := RError{
		IncidentID: &incidentID,
		Error:      incidentDetails,
		Msg:        userMsg,
		Code:       code,
	}

	getLogger().Post("com.pantahub-base.incidents", structs.Map(&rError))

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
