//
// Copyright 2017  Pantacor Ltd.
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

// Package logs provides the abstract logging infrastructure for pantahub
// logging endpoint as well as backends for elastic and mgo.
//
// Logs offers a simple logging service for Pantahub powered devices and apps.
// To post new log entries use the POST method on the main endpoint
// To page through log entries and sort etc. check the GET method
package logs

//
import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

type logsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mongoClient    *mongo.Client
	backend        LogsBackend
}

// LogsFilter uses a prototype LogsEntry instance to filter
// the values. It honours the string fields: Device, Owner,
// Source, Level and Text, where a non-empty field will
// make the backend filter results by the field.
type LogsFilter *LogsEntry

// LogsSort is about a map of sort fields prefixed with '-'
// if the order of this field should be descending (like mgo)
type LogsSort []string

type LogsEntry struct {
	Id          primitive.ObjectID `json:"id,omitempty" bson:"_id,omitempty"`
	Device      string             `json:"dev,omitempty" bson:"dev"`
	Owner       string             `json:"own,omitempty" bson:"own"`
	TimeCreated time.Time          `json:"time-created,omitempty" bson:"time-created"`
	LogTSec     int64              `json:"tsec,omitempty" bson:"tsec"`
	LogTNano    int64              `json:"tnano,omitempty" bson:"tnano"`
	LogSource   string             `json:"src,omitempty" bson:"src"`
	LogLevel    string             `json:"lvl,omitempty" bson:"lvl"`
	LogText     string             `json:"msg,omitempty" bson:"msg"`
}

type LogsPager struct {
	Start      int64        `json:"start"`
	Page       int64        `json:"page"`
	Count      int64        `json:"count"`
	NextCursor string       `json:"next-cursor,omitempty"`
	Entries    []*LogsEntry `json:"entries,omitempty"`
}

type LogsBackend interface {
	getLogs(start int64, page int64, beforeOrafter *time.Time, after bool,
		query LogsFilter, sort LogsSort, cursor bool) (*LogsPager, error)
	getLogsByCursor(nextCursor string) (*LogsPager, error)
	postLogs(e []LogsEntry) error
	register() error
	unregister(deleteIndices bool) error
}

var ErrCursorTimedOut error = errors.New("Cursor Invalid or expired.")
var ErrCursorNotImplemented error = errors.New("Cursor not supported by backend.")

type LogsCursorClaim struct {
	NextCursor string `json:"next-cursor"`
	jwtgo.StandardClaims
}

//
// ## GET /logs/
//   Post one or many log entries as an error of LogEntry
//   Page through your logs.
//
//   Context:
//      Can be called in user context
//
//   Paging Parameter:
//     - start: list position to start page; either number or ID or
//	            "<tsec>.<tnano>" of log entry
//     - page: length of page
//
//   Filter Paramters:
//     - dev: comma separated list of device prns  to include
//     - lvl: comma separated list of log levels
//     - src: comma separated list of sources
//
//   Sorting Parameters:
//     - sort: common list of items of "tsec,tnano,device,src,lvl,time-created"
//             you can use - on each individual item to reverse order
//
//   Cursor Parameters:
//     - cursor: true in case you want us to return a cursor ID as well.
//
func (a *logsApp) handle_getlogs(w rest.ResponseWriter, r *rest.Request) {

	var result *LogsPager
	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	r.ParseForm()

	startParam := r.FormValue("start")
	pageParam := r.FormValue("page")

	startParamInt := int64(0)
	if startParam != "" {
		var p int
		p, err = strconv.Atoi(startParam)
		startParamInt = int64(p)
	}
	if err != nil {
		rest.Error(w, "Bad 'start' parameter", http.StatusBadRequest)
		return
	}

	pageParamInt := int64(50)
	if pageParam != "" {
		var p int
		p, err = strconv.Atoi(pageParam)
		pageParamInt = int64(p)
	}
	if err != nil {
		rest.Error(w, "Bad 'page' parameter", http.StatusBadRequest)
		return
	}

	sourceParam := r.FormValue("src")
	deviceParam := r.FormValue("dev")
	levelParam := r.FormValue("lvl")

	filter := &LogsEntry{
		Owner:     own.(string),
		LogLevel:  levelParam,
		LogSource: sourceParam,
		Device:    deviceParam,
	}

	logsSort := LogsSort{}
	sortParam := r.FormValue("sort")

	sorts := strings.Split(sortParam, ",")
	for _, v := range sorts {
		switch v1 := strings.TrimPrefix(v, "-"); v1 {
		case "lvl":
			fallthrough
		case "dev":
			fallthrough
		case "tsec":
			fallthrough
		case "tnano":
			fallthrough
		case "time-created":
			fallthrough
		case "src":
			logsSort = append(logsSort, v)
		}
	}

	var beforeOrAfter *time.Time
	var after bool

	after = true
	beforeParam := r.FormValue("before")
	afterParam := r.FormValue("after")

	if beforeParam != "" {
		t, err := time.Parse(time.RFC3339, beforeParam)
		if err != nil {
			rest.Error(w, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
			return
		}
		beforeOrAfter = &t
		after = false
	} else if afterParam != "" {
		t, err := time.Parse(time.RFC3339, afterParam)
		if err != nil {
			rest.Error(w, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
			return
		}
		beforeOrAfter = &t
		after = true
	}

	cursor := r.FormValue("cursor") != ""
	result, err = a.backend.getLogs(startParamInt, pageParamInt, beforeOrAfter, after, filter, logsSort, cursor)

	if err != nil {
		rest.Error(w, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
		return
	}

	if result.NextCursor != "" {
		jwtSecret := []byte(utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET))

		claims := LogsCursorClaim{
			NextCursor: result.NextCursor,
			StandardClaims: jwtgo.StandardClaims{
				ExpiresAt: time.Now().Add(time.Duration(time.Minute * 2)).Unix(),
				IssuedAt:  time.Now().Unix(),
				Audience:  own.(string),
			},
		}
		token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claims)
		ss, err := token.SignedString(jwtSecret)
		if err != nil {
			rest.Error(w, "ERROR: signing scrollid token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		result.NextCursor = ss
	}

	w.WriteJson(result)
}

func (a *logsApp) handle_getlogscursor(w rest.ResponseWriter, r *rest.Request) {

	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	jsonBody := map[string]interface{}{}
	err = r.DecodeJsonPayload(&jsonBody)
	if err != nil {
		rest.Error(w, "Error decoding json request body: "+err.Error(), http.StatusBadRequest)
	}

	var nextCursorJWT string
	nextCursor := jsonBody["next-cursor"]
	if nextCursor == nil {
		nextCursorJWT = ""
	} else {
		nextCursorJWT = nextCursor.(string)
	}
	// if body doesnt have the cursor lets try query
	if nextCursorJWT == "" {
		r.ParseForm()
		nextCursorJWT = r.FormValue("next-cursor")

	}
	token, err := jwtgo.ParseWithClaims(nextCursorJWT, &LogsCursorClaim{}, func(token *jwtgo.Token) (interface{}, error) {
		return []byte(utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET)), nil
	})

	if err != nil {
		rest.Error(w, "Error decoding JWT token for next-cursor: "+err.Error(), http.StatusForbidden)
		return
	}

	if claims, ok := token.Claims.(*LogsCursorClaim); ok && token.Valid {
		var result *LogsPager

		caller := claims.StandardClaims.Audience
		if caller != own {
			rest.Error(w, "Calling user does not match owner of cursor-next", http.StatusForbidden)
			return
		}
		nextCursor := claims.NextCursor
		result, err = a.backend.getLogsByCursor(nextCursor)

		if err != nil {
			rest.Error(w, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
			return
		}

		if result.NextCursor != "" {
			jwtSecret := []byte(utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET))

			claims := LogsCursorClaim{
				NextCursor: result.NextCursor,
				StandardClaims: jwtgo.StandardClaims{
					ExpiresAt: time.Now().Add(time.Duration(time.Minute * 2)).Unix(),
					IssuedAt:  time.Now().Unix(),
					Audience:  own.(string),
				},
			}
			token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claims)
			ss, err := token.SignedString(jwtSecret)
			if err != nil {
				rest.Error(w, "ERROR: signing scrollid token: "+err.Error(), http.StatusInternalServerError)
				return
			}
			result.NextCursor = ss
		}

		w.WriteJson(result)
		return
	}

	rest.Error(w, "Unexpected Code", http.StatusInternalServerError)
	return
}

func unmarshalBody(body []byte) ([]LogsEntry, error) {
	entries := make([]LogsEntry, 1)

	err := json.Unmarshal(body, &entries)

	// if array parse fail, we try direct...
	if err != nil {
		err = json.Unmarshal(body, &entries[0])
	}

	// if all fail, we bail...
	if err != nil {
		return nil, err
	}

	return entries, nil
}

//
// ## POST /logs/
//   Post one or many log entries as an error of LogEntry
func (a *logsApp) handle_postlogs(w rest.ResponseWriter, r *rest.Request) {

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "DEVICE" {
		rest.Error(w, "Need to be logged in as DEVICE to post logs", http.StatusForbidden)
		return
	}

	device, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as device with owner", http.StatusForbidden)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		rest.Error(w, "Error reading logs body", http.StatusBadRequest)
		return
	}

	entries, err := unmarshalBody(body)

	if err != nil {
		rest.Error(w, "Error parsing logs body: "+err.Error(), http.StatusBadRequest)
		return
	}

	newEntries := []LogsEntry{}

	for _, v := range entries {
		v.Id, err = primitive.ObjectIDFromHex(bson.NewObjectId().Hex())
		if err != nil {
			rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
			return
		}
		v.Device = device.(string)
		v.Owner = owner.(string)
		v.TimeCreated = time.Now()
		if v.LogLevel == "" {
			v.LogLevel = "INFO"
		}
		newEntries = append(newEntries, v)
	}

	err = a.backend.postLogs(newEntries)
	if err != nil {
		rest.Error(w, "Error posting logs "+err.Error(), http.StatusInternalServerError)
		log.Println("ERROR: Error posting logs " + err.Error())
		return
	}

	w.WriteJson(newEntries)
}

func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *logsApp {

	var err error

	app := new(logsApp)
	app.jwt_middleware = jwtMiddleware
	app.mongoClient = mongoClient

	app.backend, err = NewElasticLogger()

	if err == nil {
		err = app.backend.register()
	}
	if err != nil {
		log.Println("INFO: Elastic Logger failed to start: " + err.Error())
		log.Println("INFO: Elastic Logger not available; trying other options ...")

		app.backend, err = NewMgoLogger(mongoClient)
		if err == nil {
			err = app.backend.register()
		}
	} else {
		log.Println("INFO: Elastic Logger started.")
	}

	if err != nil {
		log.Println("ERROR: Final Logger also failed to start: " + err.Error())
		log.Println("INFO: will log to stdout now ...")
	}

	app.Api = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/logs:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "logs"})

	app.Api.Use(rest.DefaultCommonStack...)

	// we allow calls from other domains to allow webapps; XXX: review
	app.Api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwt_middleware,
	})

	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	api_router, _ := rest.MakeRouter(
		rest.Get("/", app.handle_getlogs),
		rest.Get("/cursor", app.handle_getlogscursor),
		rest.Post("/cursor", app.handle_getlogscursor),
		rest.Post("/", app.handle_postlogs),
	)
	app.Api.SetApp(api_router)

	return app
}
