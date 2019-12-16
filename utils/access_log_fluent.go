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
	MAX_READ_BODY_SIZE int = int(math.Pow(2, 24)) // 16M
	READ_BLOCK_SIZE    int = int(math.Pow(2, 16)) // 64k
)

type ResponseWriterFunc func(string, rest.Request)

type ResponseWriterWrapper struct {
	responseWriter rest.ResponseWriter
	RequestBody    []byte
	ResponseBody   []byte
}

func (r *ResponseWriterWrapper) Count() uint64 {
	return r.responseWriter.Count()
}

func (r *ResponseWriterWrapper) EncodeJson(v interface{}) ([]byte, error) {
	return r.responseWriter.EncodeJson(v)
}

func (r *ResponseWriterWrapper) Header() http.Header {
	return r.responseWriter.Header()
}

func (r *ResponseWriterWrapper) WriteHeader(code int) {
	r.responseWriter.WriteHeader(code)
}

func (r *ResponseWriterWrapper) WriteJson(v interface{}) error {
	c, _ := json.Marshal(v)
	r.ResponseBody = c
	return r.responseWriter.WriteJson(v)
}

func (r *ResponseWriterWrapper) Write(c []byte) (int, error) {
	r.ResponseBody = c
	return r.responseWriter.Write(c)
}

func NewResponseWriterWrapper(w rest.ResponseWriter) *ResponseWriterWrapper {
	return &ResponseWriterWrapper{responseWriter: w}
}

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
			log.Printf("INFO: fluent logging enabled for endpoint %s; %s: %s, %s: %d\n", mw.Prefix, ENV_FLUENT_HOST, host, ENV_FLUENT_PORT, port)
		} else {
			log.Printf("WARNING: fluent logging disabled for endpoint %s; set %s to enable it.\n\tTo enable fluentd, set at least FLUENTD_HOST environment", mw.Prefix, ENV_FLUENT_HOST)
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
		bodySize, err := strconv.ParseFloat(r.Header.Get("Content-Length"), 64)
		if err != nil {
			rest.Error(w, "Error when parsing Content-Length header", http.StatusBadRequest)
			return
		}

		if int(bodySize) > MAX_READ_BODY_SIZE {
			rest.Error(w, "Body is too large", http.StatusRequestEntityTooLarge)
			return
		}

		requestBody := []byte{}
		responseBody := []byte{}

		ct := r.Header.Get("Content-Type")

		// only read body if the content is json to avoid read binary or uploaded files
		if ct == "application/json" {
			responseWrapper := NewResponseWriterWrapper(w)

			// read blocks of 64k
			for {
				readBuf := make([]byte, READ_BLOCK_SIZE)
				n, _ := r.Request.Body.Read(readBuf)
				requestBody = append(requestBody, readBuf[:n]...)
				if n < READ_BLOCK_SIZE {
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
		if len(responseBody) > MAX_READ_BODY_SIZE {
			responseBody = responseBody[:MAX_READ_BODY_SIZE]
		}

		// limit request size to MAX_READ_BODY_SIZE
		if len(requestBody) > MAX_READ_BODY_SIZE {
			requestBody = requestBody[:MAX_READ_BODY_SIZE]
		}

		logRec := mw.makeAccessLogFluentRecord(w, responseBody, r, requestBody)

		m := structs.Map(logRec)

		err = mw.Logger.Post(mw.Tag, m)
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
	Endpoint       string
	Hostname       string
	HttpMethod     string
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
		HttpMethod:     r.Method,
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
		RequestBody:    mw.recordableBody(requestBody),
		ResponseBody:   mw.recordableBody(responseBody),
	}
}

func (r *AccessLogFluentMiddleware) recordableBody(v []byte) string {
	return string(v)
}

func (r *AccessLogFluentRecord) asJson() []byte {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return b
}
