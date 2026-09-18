//
// Copyright (c) 2017-2026 Pantacor Ltd.
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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

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
	Code       int    `json:"code,omitempty"`
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

func getLogger() *fluent.Fluent {
	portStr := GetEnv(EnvFluentPort)
	if portStr == "" {
		return nil
	}

	if logger == nil {
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

func LogError(errorMsg, userMsg string, code int) {
	incidentID := time.Now().UnixNano()

	incidentStr := fmt.Sprintf("REST-ERR-ID-%d", incidentID)
	incidentDetails := fmt.Sprintf("ERROR| %s: %s", incidentStr, errorMsg)
	log.Printf("ERROR: %s", incidentDetails)

	rError := RError{
		IncidentID: &incidentID,
		Error:      incidentDetails,
		Msg:        userMsg,
		Code:       code,
	}

	err := getLogger().Post("com.pantahub-base.incidents", structs.Map(&rError))
	if err != nil {
		log.Println(err)
	}
}

// MintRestError mints an incident id, logs and forwards the detail to fluentd, and
// returns the client-safe body. Shared by go-json-rest and utils/echoutil.
func MintRestError(errorStr, userMsg string, code int) RError {
	incidentID := time.Now().UnixNano()

	incidentStr := fmt.Sprintf("REST-ERR-ID-%d", incidentID)
	incidentDetails := fmt.Sprintf("ERROR| %s: %s", incidentStr, errorStr)
	log.Printf("ERROR: %s", incidentDetails)

	rError := RError{
		IncidentID: &incidentID,
		Error:      incidentDetails,
		Msg:        userMsg,
		Code:       code,
	}

	if l := getLogger(); l != nil {
		err := l.Post("com.pantahub-base.incidents", structs.Map(&rError))
		if err != nil {
			log.Println(err)
		}
	}

	return RError{
		Error: incidentStr,
		Msg:   userMsg,
		Code:  code,
	}
}

func HttpErrorWrapper(w http.ResponseWriter, errorStr string, code int) {
	incidentID := time.Now().UnixNano()

	incidentStr := fmt.Sprintf("REST-ERR-ID-%d", incidentID)
	incidentDetails := fmt.Sprintf("ERROR| %s: %s", incidentStr, errorStr)
	log.Printf("ERROR: %s", incidentDetails)

	rError := RError{
		IncidentID: &incidentID,
		Error:      incidentDetails,
		Msg:        "",
		Code:       code,
	}

	err := getLogger().Post("com.pantahub-base.incidents", structs.Map(&rError))
	if err != nil {
		log.Println(err)
	}

	w.WriteHeader(code)
	body := RError{
		Error: incidentStr,
		Msg:   "",
		Code:  code,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		log.Println(err)
		panic(err)
	}
	_, err = w.Write(payload)
	if err != nil {
		log.Println(err)
		panic(err)
	}
}
