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
// Package utils offers reusable utils for pantahub-base developers
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//

package utils

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/fatih/structs"
	"github.com/fluent/fluent-logger-golang/fluent"
	jwtgo "github.com/golang-jwt/jwt/v5"
)

var (
	maxReadBodySize int = int(math.Pow(2, 24)) // 16M
	readBlockSize   int = int(math.Pow(2, 16)) // 64k
)

// AccessLogFluentMiddleware produces the access log with records written as JSON. This middleware
// depends on TimerMiddleware and RecorderMiddleware that must be in the wrapped middlewares. It
// also uses request.Env["REMOTE_USER"].(string) set by the auth middlewares.
type AccessLogFluentMiddleware struct {
	Logger    *fluent.Fluent
	Prefix    string
	Tag       string
	Namespace string
	Hostname  string
}

// Init sets up the fluent logger and defaults; false when FLUENT_PORT is empty.
// Init, LogsBody, BuildAccessLogFluentRecord and Post are shared with utils/echoutil.
func (mw *AccessLogFluentMiddleware) Init() bool {
	// set the default Logger
	if mw.Logger == nil {
		var err error
		var port int
		var host string

		portStr := GetEnv(EnvFluentPort)
		if portStr == "" {
			return false
		}

		port, err = strconv.Atoi(portStr)
		if err != nil {
			log.Fatalln("FATAL: cannot read fluent logger settings: " + err.Error())
		}

		host = GetEnv(EnvFluentHost)

		if host != "" {
			for i := 0; i < 10; i++ {
				mw.Logger, err = fluent.New(fluent.Config{FluentPort: port, FluentHost: host})
				if err == nil {
					break
				}
				log.Printf("WARNING: couldnt instantiate fluent logger (round %d/10): %s\n", i, err.Error())
				time.Sleep(time.Duration(6 * time.Second))
			}
			if err != nil {
				log.Fatalln("FATAL: couldn't instantiate fluent logger: " + err.Error())
			}
			log.Printf("INFO: fluent logging enabled for endpoint %s; %s: %s, %s: %d\n", mw.Prefix, EnvFluentHost, host, EnvFluentPort, port)
		} else {
			log.Printf("WARNING: fluent logging disabled for endpoint %s; set %s to enable it.\n\tTo enable fluentd, set at least FLUENTD_HOST environment", mw.Prefix, EnvFluentHost)
		}
	}

	if mw.Prefix == "" {
		p := string("NOENDPOINT")
		mw.Prefix = p
	}

	if mw.Tag == "" {
		t := "com.pantahub-base.access"
		mw.Tag = t
	}

	if mw.Hostname == "" {
		mw.Hostname = GetEnv(EnvHostName)
	}

	if mw.Namespace == "" {
		mw.Namespace = GetEnv(EnvK8SNamespace)
	}

	return true
}

// LogsBody reports whether bodies are logged (JSON only, PANTAHUB_LOG_BODY=true).
func (mw *AccessLogFluentMiddleware) LogsBody(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "application/json" && GetEnv(EnvPantahubLogBody) == "true"
}

// ReadAndRestoreBody reads the whole request body and puts back an equivalent
// reader, so the handler still sees it.
func ReadAndRestoreBody(r *http.Request) []byte {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// Handle error
		log.Printf("Error reading body: %v", err)
		// You might want to return or handle the error appropriately
	}

	// Restore the body for future reads
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return bodyBytes
}

// Post sends a record to fluentd.
func (mw *AccessLogFluentMiddleware) Post(rec *AccessLogFluentRecord) {
	m := structs.Map(rec)

	err := mw.Logger.Post(mw.Tag, m)
	if err != nil {
		log.Println("WARNING: error posting logs to fluentd: " + err.Error())
	}
}

// JSONLog json payload for logs
type JSONLog struct {
	Log    string    `json:"log"`
	Stream string    `json:"stream"`
	Time   time.Time `json:"time"`
}

// AccessLogFluentRecord is the data structure used by AccessLogFluentMiddleware to create the JSON
// records. (Public for documentation only, no public method uses it)
type AccessLogFluentRecord struct {
	Endpoint       string
	Hostname       string
	HTTPMethod     string
	Namespace      string
	RemoteUser     string
	RequestHeaders map[string]interface{}
	RequestBody    string
	RequestParams  map[string]interface{}
	RequestURI     string
	ResponseBody   string
	ResponseSize   uint64
	ResponseTime   int64
	StatusCode     int
	Timestamp      int64
	UserAgent      string
}

// BuildAccessLogFluentRecord builds a record from r.Env-style values. r must carry
// the prefix-stripped URL the service saw.
func BuildAccessLogFluentRecord(mw *AccessLogFluentMiddleware, r *http.Request, env map[string]interface{}, responseSize uint64, requestBody, responseBody []byte) *AccessLogFluentRecord {
	var timestamp *time.Time
	if env["START_TIME"] != nil {
		timestamp = env["START_TIME"].(*time.Time)
	}

	var statusCode int
	if env["STATUS_CODE"] != nil {
		statusCode = env["STATUS_CODE"].(int)
	}

	var responseTime *time.Duration
	if env["ELAPSED_TIME"] != nil {
		responseTime = env["ELAPSED_TIME"].(*time.Duration)
	}

	var remoteUser string
	if env["REMOTE_USER"] != nil {
		remoteUser = env["REMOTE_USER"].(string)
	} else if env["JWT_PAYLOAD"] != nil {
		payload := env["JWT_PAYLOAD"].(jwtgo.MapClaims)
		if payload["id"] != nil {
			remoteUser = payload["id"].(string)
		}
		if payload["aud"] != nil {
			remoteUser = payload["aud"].(string) + "(" + remoteUser + ")"
		}
	}
	// msgpack does not like type map[string][]string; hence we
	// help by using interface{} value type instead
	reqMap := map[string]interface{}{}
	for k, v := range r.Header {
		if k == "Authorization" {
			continue
		}
		reqMap[k] = v
	}

	reqParams := map[string]interface{}{}
	for k, v := range r.URL.Query() {
		reqParams[k] = v
	}

	return &AccessLogFluentRecord{
		Endpoint:       mw.Prefix,
		Hostname:       mw.Hostname,
		HTTPMethod:     r.Method,
		Namespace:      mw.Namespace,
		RemoteUser:     remoteUser,
		RequestHeaders: reqMap,
		RequestParams:  reqParams,
		RequestURI:     r.URL.RequestURI(),
		ResponseSize:   responseSize,
		ResponseTime:   responseTime.Nanoseconds(),
		StatusCode:     statusCode,
		Timestamp:      timestamp.Unix(),
		UserAgent:      r.UserAgent(),
		RequestBody:    string(requestBody),
		ResponseBody:   string(responseBody),
	}
}

func (r *AccessLogFluentRecord) asJSON() []byte {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return b
}
