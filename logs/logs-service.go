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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type logsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
}

// LogsFilter uses a prototype LogsEntry instance to filter
// the values. It honours the string fields: Device, Owner,
// Source, Level and Text, where a non-empty field will
// make the backend filter results by the field.
type LogsFilter LogsEntry

// LogsSort is about a map of sort fields prefixed with '-'
// if the order of this field should be descending (like mgo)
type LogsSort []string

type LogsEntry struct {
	Id          bson.ObjectId `json:"id,omitempty" bson:"_id,omitempty"`
	Device      string        `json:"dev" bson:"dev"`
	Owner       string        `json:"own" bson:"own"`
	TimeCreated time.Time     `json:"time-created" bson:"time-created"`
	LogTSec     int           `json:"tsec" bson:"tsec"`
	LogTNano    int           `json:"tnano" bson:"tnano"`
	LogSource   string        `json:"src" bson:"src"`
	LogLevel    string        `json:"lvl" bson:"lvl"`
	LogText     string        `json:"msg" bson:"msg"`
}

type LogsPager struct {
	Start   int         `json:"start"`
	Page    int         `json:"page"`
	Count   int         `json:"count"`
	Entries []LogsEntry `json:"entries"`
}

type LogsBackend interface {
	getLogs(start int, page int, query *LogsFilter, sort *LogsSort) (*LogsPager, error)
	doLog(e []*LogsEntry) error
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
//     - sort: comman list of items of "tsec,tnano,device,src,lvl,time-created"
//             you can use - on each individual item to reverse order
//
func (a *logsApp) handle_getlogs(w rest.ResponseWriter, r *rest.Request) {

	var result LogsPager
	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	r.ParseForm()

	startParam := r.FormValue("start")
	pageParam := r.FormValue("page")

	sourceParam := r.FormValue("src")
	deviceParam := r.FormValue("dev")
	levelParam := r.FormValue("lvl")

	sortParam := r.FormValue("sort")

	startParamInt := 0
	if startParam != "" {
		startParamInt, err = strconv.Atoi(startParam)
	}
	if err != nil {
		rest.Error(w, "Bad 'start' parameter", http.StatusBadRequest)
		return
	}

	pageParamInt := 50
	if pageParam != "" {
		pageParamInt, err = strconv.Atoi(pageParam)
	}
	if err != nil {
		rest.Error(w, "Bad 'page' parameter", http.StatusBadRequest)
		return
	}

	arr := make([]string, 0)

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
			arr = append(arr, v)
		}
	}
	sortStr := strings.Join(arr, ",")

	collLogs := a.mgoSession.DB("").C("pantahub_logs")

	if collLogs == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	findFilter := bson.M{
		"own": own,
	}

	if levelParam != "" {
		findFilter["lvl"] = levelParam
	}
	if deviceParam != "" {
		findFilter["dev"] = deviceParam
	}
	if sourceParam != "" {
		findFilter["src"] = sourceParam
	}

	if sortStr == "" {
		sortStr =
			"-time-created"
	}

	q := collLogs.Find(findFilter).Sort(sortStr)

	result.Count, err = q.Count()
	result.Start = startParamInt
	result.Page = pageParamInt

	if err != nil {
		rest.Error(w, "Error with Database count", http.StatusInternalServerError)
		return
	}

	entries := []LogsEntry{}
	err = q.Skip(startParamInt).Limit(pageParamInt).All(&entries)

	if err != nil {
		rest.Error(w, "Error with Database count", http.StatusInternalServerError)
		return
	}

	result.Entries = entries

	w.WriteJson(result)
}

//
// ## POST /logs/
//   Post one or many log entries as an error of LogEntry
func (a *logsApp) handle_postlogs(w rest.ResponseWriter, r *rest.Request) {

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if authType != "DEVICE" {
		rest.Error(w, "Need to be logged in as DEVICE to post logs", http.StatusForbidden)
		return
	}

	device, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["owner"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as device with owner", http.StatusForbidden)
		return
	}

	entries := make([]LogsEntry, 1)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		rest.Error(w, "Error reading body", http.StatusBadRequest)
		log.Println("Error reading body: " + err.Error())
		return
	}

	err = json.Unmarshal(body, &entries)

	// if array parse fail, we try direct...
	if err != nil {
		err = json.Unmarshal(body, &entries[0])
	}

	// if all fail, we bail...
	if err != nil {
		rest.Error(w, "Error parsing request", http.StatusBadRequest)
		log.Println("Error parsing request: " + err.Error())
		return
	}

	collLogs := a.mgoSession.DB("").C("pantahub_logs")

	if collLogs == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	newEntries := []LogsEntry{}

	for _, v := range entries {
		v.Id = bson.NewObjectId()
		v.Device = device.(string)
		v.Owner = owner.(string)
		v.TimeCreated = time.Now()
		if v.LogLevel == "" {
			v.LogLevel = "INFO"
		}
		err := collLogs.Insert(&v)
		if err != nil {
			rest.Error(w, "Error inserting log entry", http.StatusForbidden)
			return
		}
		newEntries = append(newEntries, v)
		log.Println("inserted: " + v.Id.Hex())
	}

	w.WriteJson(newEntries)
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *logsApp {

	app := new(logsApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	app.mgoSession = session

	index := mgo.Index{
		Key:        []string{"own"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}

	err := app.mgoSession.DB("").C("pantahub_logs").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_logs: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"dev"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}
	err = app.mgoSession.DB("").C("pantahub_logs").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_logs: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"time-created"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}
	err = app.mgoSession.DB("").C("pantahub_logs").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_logs: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"tsec", "tnano"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}
	err = app.mgoSession.DB("").C("pantahub_logs").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_logs: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"lvl"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}

	err = app.mgoSession.DB("").C("pantahub_logs").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_logs: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"dev", "own", "time-created"},
		Unique:     false,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     false,
	}

	err = app.mgoSession.DB("").C("pantahub_logs").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_logs: " + err.Error())
		return nil
	}

	app.Api = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{Logger: log.New(os.Stdout,
		"/logs:", log.Lshortfile)})
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

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwt_middleware,
	})

	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	api_router, _ := rest.MakeRouter(
		rest.Get("/", app.handle_getlogs),
		rest.Post("/", app.handle_postlogs),
	)
	app.Api.SetApp(api_router)

	return app
}
