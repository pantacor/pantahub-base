//
// Package utils offers reusable utils for pantahub-base developers
//
// (c) Pantacor Ltd, 2018
// License: Apache 2.0 (see COPYRIGHT)
//
package utils

import (
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/fatih/structs"
	"github.com/fluent/fluent-logger-golang/fluent"
)

// AccessLogJsonMiddleware produces the access log with records written as JSON. This middleware
// depends on TimerMiddleware and RecorderMiddleware that must be in the wrapped middlewares. It
// also uses request.Env["REMOTE_USER"].(string) set by the auth middlewares.
type AccessLogFluentMiddleware struct {
	Logger    *fluent.Fluent
	Prefix    string
	Tag       string
	Namespace string
	Hostname  string
}

// MiddlewareFunc makes AccessLogJsonMiddleware implement the Middleware interface.
func (mw *AccessLogFluentMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {

	// set the default Logger
	if mw.Logger == nil {
		var err error
		var port int
		var host string

		portStr := GetEnv(ENV_FLUENT_PORT)
		port, err = strconv.Atoi(portStr)
		if err != nil {
			log.Fatalln("FATAL: cannot read fluent logger settings: " + err.Error())
		}

		host = GetEnv(ENV_FLUENT_HOST)

		if host != "" {
			mw.Logger, err = fluent.New(fluent.Config{FluentPort: port, FluentHost: host})
			if err != nil {
				log.Fatalln("FATAL: cannot instantiate fluent logger: " + err.Error())
				return nil
			}
			log.Printf("INFO: fluent logging enabled for endpoint %s; %s: %s, %s: %d\n", mw.Prefix, ENV_FLUENT_HOST, host, ENV_FLUENT_PORT, port)
		} else {
			log.Printf("WARNING: fluent logging disabled for endpoint %s; set %s to enable it.\n", mw.Prefix, ENV_FLUENT_HOST)
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
		mw.Hostname = GetEnv(ENV_HOSTNAME)
	}

	if mw.Namespace == "" {
		mw.Namespace = GetEnv(ENV_K8S_NAMESPACE)
	}

	return func(w rest.ResponseWriter, r *rest.Request) {
		// call the handler
		h(w, r)

		// if fluent logging is disabled in config, just do nothing...
		if mw.Logger == nil {
			return
		}

		logRec := mw.makeAccessLogFluentRecord(r)
		logRec.ResponseSize = w.Count()

		m := structs.Map(logRec)

		err := mw.Logger.Post(mw.Tag, m)

		if err != nil {
			log.Println("WARNING: error posting logs to fluentd: " + err.Error())
		}
	}
}

type JsonLog struct {
	Log    string    `json:"log"`
	Stream string    `json:"stream"`
	Time   time.Time `json:"time"`
}

// AccessLogFluentRecord is the data structure used by AccessLogFluentMiddleware to create the JSON
// records. (Public for documentation only, no public method uses it)
type AccessLogFluentRecord struct {
	Timestamp    int64
	StatusCode   int
	ResponseTime int64
	HttpMethod   string
	RequestURI   string
	RemoteUser   string
	UserAgent    string
	Hostname     string
	Namespace    string
	Endpoint     string
	ResponseSize uint64
	ReqHeaders   map[string]interface{}
}

func (mw *AccessLogFluentMiddleware) makeAccessLogFluentRecord(r *rest.Request) *AccessLogFluentRecord {

	var timestamp *time.Time
	if r.Env["START_TIME"] != nil {
		timestamp = r.Env["START_TIME"].(*time.Time)
	}

	var statusCode int
	if r.Env["STATUS_CODE"] != nil {
		statusCode = r.Env["STATUS_CODE"].(int)
	}

	var responseTime *time.Duration
	if r.Env["ELAPSED_TIME"] != nil {
		responseTime = r.Env["ELAPSED_TIME"].(*time.Duration)
	}

	var remoteUser string
	if r.Env["REMOTE_USER"] != nil {
		remoteUser = r.Env["REMOTE_USER"].(string)
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

	return &AccessLogFluentRecord{
		Timestamp:    timestamp.Unix(),
		StatusCode:   statusCode,
		ResponseTime: responseTime.Nanoseconds(),
		HttpMethod:   r.Method,
		RequestURI:   r.URL.RequestURI(),
		RemoteUser:   remoteUser,
		UserAgent:    r.UserAgent(),
		Hostname:     mw.Hostname,
		Namespace:    mw.Namespace,
		Endpoint:     mw.Prefix,
		ReqHeaders:   reqMap,
	}
}

func (r *AccessLogFluentRecord) asJson() []byte {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return b
}
