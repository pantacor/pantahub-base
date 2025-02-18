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
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"github.com/fatih/structs"
	"github.com/fluent/fluent-logger-golang/fluent"
)

var (
	maxReadBodySize int = int(math.Pow(2, 24)) // 16M
	readBlockSize   int = int(math.Pow(2, 16)) // 64k
)

// ResponseWriterFunc rest http writer func
type ResponseWriterFunc func(string, rest.Request)

// ResponseWriterWrapper response writer wrapper for rest
type ResponseWriterWrapper struct {
	responseWriter rest.ResponseWriter
	RequestBody    []byte
	ResponseBody   []byte
}

// Count count length of writer
func (r *ResponseWriterWrapper) Count() uint64 {
	return r.responseWriter.Count()
}

// EncodeJson encode json using a interface
func (r *ResponseWriterWrapper) EncodeJson(v interface{}) ([]byte, error) {
	return r.responseWriter.EncodeJson(v)
}

// Header get writer header
func (r *ResponseWriterWrapper) Header() http.Header {
	return r.responseWriter.Header()
}

// WriteHeader write a header code
func (r *ResponseWriterWrapper) WriteHeader(code int) {
	r.responseWriter.WriteHeader(code)
}

// WriteJson write a json response
func (r *ResponseWriterWrapper) WriteJson(v interface{}) error {
	c, _ := json.Marshal(v)
	r.ResponseBody = c
	return r.responseWriter.WriteJson(v)
}

func (r *ResponseWriterWrapper) Write(c []byte) (int, error) {
	r.ResponseBody = c
	return r.responseWriter.Write(c)
}

// NewResponseWriterWrapper create a new wrapper for writer
func NewResponseWriterWrapper(w rest.ResponseWriter) *ResponseWriterWrapper {
	return &ResponseWriterWrapper{responseWriter: w}
}

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

// MiddlewareFunc makes AccessLogJsonMiddleware implement the Middleware interface.
func (mw *AccessLogFluentMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {

	// set the default Logger
	if mw.Logger == nil {
		var err error
		var port int
		var host string

		portStr := GetEnv(EnvFluentPort)
		if portStr == "" {
			return func(w rest.ResponseWriter, r *rest.Request) {
				h(w, r)
			}
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

	return func(w rest.ResponseWriter, r *rest.Request) {
		requestBody := []byte{}
		responseBody := []byte{}

		ct := r.Header.Get("Content-Type")

		// only read body if the content is json to avoid read binary or uploaded files
		if ct == "application/json" && GetEnv(EnvPantahubLogBody) == "true" {
			responseWrapper := NewResponseWriterWrapper(w)

			// read blocks of 64k
			for {
				readBuf := make([]byte, readBlockSize)
				n, _ := r.Request.Body.Read(readBuf)
				requestBody = append(requestBody, readBuf[:n]...)
				if n < readBlockSize {
					break
				}
			}

			r.Request.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))

			// call the handler
			h(responseWrapper, r)

			responseBody = responseWrapper.ResponseBody
		} else {
			// call the handler
			h(w, r)
		}

		// if fluent logging is disabled in config, just do nothing...
		if mw.Logger == nil {
			return
		}

		// limit response size to MAX_READ_BODY_SIZE
		if len(responseBody) > maxReadBodySize {
			responseBody = responseBody[:maxReadBodySize]
		}

		// limit request size to MAX_READ_BODY_SIZE
		if len(requestBody) > maxReadBodySize {
			requestBody = requestBody[:maxReadBodySize]
		}

		logRec := mw.makeAccessLogFluentRecord(w, responseBody, r, requestBody)

		m := structs.Map(logRec)

		err := mw.Logger.Post(mw.Tag, m)
		if err != nil {
			log.Println("WARNING: error posting logs to fluentd: " + err.Error())
		}
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

func (mw *AccessLogFluentMiddleware) makeAccessLogFluentRecord(w rest.ResponseWriter, responseBody []byte, r *rest.Request, requestBody []byte) *AccessLogFluentRecord {
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
	} else if r.Env["JWT_PAYLOAD"] != nil {
		payload := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)
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
		ResponseSize:   w.Count(),
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
