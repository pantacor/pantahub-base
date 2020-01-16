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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// App logs rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
	backend       Backend
}

// Filters uses a prototype Entry instance to filter
// the values. It honours the string fields: Device, Owner,
// Source, Level and Text, where a non-empty field will
// make the backend filter results by the field.
type Filters *Entry

// Sorts is about a map of sort fields prefixed with '-'
// if the order of this field should be descending (like mgo)
type Sorts []string

// Entry log entry payload
type Entry struct {
	ID          primitive.ObjectID `json:"id,omitempty" bson:"_id,omitempty"`
	Device      string             `json:"dev,omitempty" bson:"dev"`
	Owner       string             `json:"own,omitempty" bson:"own"`
	TimeCreated time.Time          `json:"time-created,omitempty" bson:"time-created"`
	LogTSec     int64              `json:"tsec,omitempty" bson:"tsec"`
	LogTNano    int64              `json:"tnano,omitempty" bson:"tnano"`
	LogSource   string             `json:"src,omitempty" bson:"src"`
	LogLevel    string             `json:"lvl,omitempty" bson:"lvl"`
	LogText     string             `json:"msg,omitempty" bson:"msg"`
}

// Pager logs pagination structure
type Pager struct {
	Start      int64    `json:"start"`
	Page       int64    `json:"page"`
	Count      int64    `json:"count"`
	NextCursor string   `json:"next-cursor,omitempty"`
	Entries    []*Entry `json:"entries,omitempty"`
}

// Backend logs interface
type Backend interface {
	getLogs(start int64, page int64, beforeOrafter *time.Time, after bool,
		query Filters, sort Sorts, cursor bool) (*Pager, error)
	getLogsByCursor(nextCursor string) (*Pager, error)
	postLogs(e []Entry) error
	register() error
	unregister(deleteIndices bool) error
}

// ErrCursorTimedOut invalid cursor error
var ErrCursorTimedOut error = errors.New("cursor Invalid or expired")

// ErrCursorNotImplemented cursor not implemented
var ErrCursorNotImplemented error = errors.New("cursor not supported by backend")

// CursorClaim claim log cursor
type CursorClaim struct {
	NextCursor string `json:"next-cursor"`
	jwtgo.StandardClaims
}

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
func (a *App) handleGetLogs(w rest.ResponseWriter, r *rest.Request) {

	var result *Pager
	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
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
		utils.RestErrorWrapper(w, "Bad 'start' parameter", http.StatusBadRequest)
		return
	}

	pageParamInt := int64(50)
	if pageParam != "" {
		var p int
		p, err = strconv.Atoi(pageParam)
		pageParamInt = int64(p)
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Bad 'page' parameter", http.StatusBadRequest)
		return
	}

	sourceParam := r.FormValue("src")
	deviceParam := r.FormValue("dev")
	deviceParam, err = a.ParseDeviceString(deviceParam)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device nicks:"+err.Error(), http.StatusBadRequest)
		return
	}
	levelParam := r.FormValue("lvl")

	filter := &Entry{
		Owner:     own.(string),
		LogLevel:  levelParam,
		LogSource: sourceParam,
		Device:    deviceParam,
	}

	logsSort := Sorts{}
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
			utils.RestErrorWrapper(w, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
			return
		}
		beforeOrAfter = &t
		after = false
	} else if afterParam != "" {
		t, err := time.Parse(time.RFC3339, afterParam)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
			return
		}
		beforeOrAfter = &t
		after = true
	}

	cursor := r.FormValue("cursor") != ""
	result, err = a.backend.getLogs(startParamInt, pageParamInt, beforeOrAfter, after, filter, logsSort, cursor)

	if err != nil {
		utils.RestErrorWrapper(w, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
		return
	}

	if result.NextCursor != "" {
		claims := CursorClaim{
			NextCursor: result.NextCursor,
			StandardClaims: jwtgo.StandardClaims{
				ExpiresAt: time.Now().Add(time.Duration(time.Minute * 2)).Unix(),
				IssuedAt:  time.Now().Unix(),
				Audience:  own.(string),
			},
		}
		token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm), claims)
		ss, err := token.SignedString(a.jwtMiddleware.Key)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR: signing scrollid token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		result.NextCursor = ss
	}

	w.WriteJson(result)
}

// ParseDeviceString : Parse Device Nicks & Device Id's from a string and replace them with device Prn
func (a *App) ParseDeviceString(devicesString string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return "", errors.New("Error with Database connectivity")
	}
	devicePrns := []string{}

	components := strings.Split(devicesString, ",")
	deviceObject := devices.Device{}
	for _, device := range components {
		hasPrefix, _ := regexp.MatchString("^prn:(.*):devices:/(.+)$", device)
		if hasPrefix {
			devicePrns = append(devicePrns, device)
			continue
		}
		deviceNick := ""
		deviceObjectID, err := primitive.ObjectIDFromHex(device)
		if err != nil {
			deviceNick = device
		}
		if deviceNick != "" {
			err = collection.FindOne(ctx,
				bson.M{
					"nick":    deviceNick,
					"garbage": bson.M{"$ne": true},
				}).
				Decode(&deviceObject)
		} else {
			err = collection.FindOne(ctx,
				bson.M{
					"_id":     deviceObjectID,
					"garbage": bson.M{"$ne": true},
				}).
				Decode(&deviceObject)
		}
		if err != nil {
			fmt.Print("Error finding device:" + device + ",err:" + err.Error())
			continue
		}
		if deviceObject.Nick != "" {
			devicePrns = append(devicePrns, deviceObject.Prn)
		}
	}
	return strings.Join(devicePrns, ","), nil
}

func (a *App) handleGetLogsCursor(w rest.ResponseWriter, r *rest.Request) {

	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	jsonBody := map[string]interface{}{}
	err = r.DecodeJsonPayload(&jsonBody)
	if err != nil {
		utils.RestErrorWrapper(w, "Error decoding json request body: "+err.Error(), http.StatusBadRequest)
		return
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

	token, err := jwtgo.ParseWithClaims(nextCursorJWT, &CursorClaim{}, func(token *jwtgo.Token) (interface{}, error) {
		return a.jwtMiddleware.Pub, nil
	})

	if err != nil {
		utils.RestErrorWrapper(w, "Error decoding JWT token for next-cursor: "+err.Error(), http.StatusForbidden)
		return
	}

	if claims, ok := token.Claims.(*CursorClaim); ok && token.Valid {
		var result *Pager

		caller := claims.StandardClaims.Audience
		if caller != own {
			utils.RestErrorWrapper(w, "Calling user does not match owner of cursor-next", http.StatusForbidden)
			return
		}
		nextCursor := claims.NextCursor
		result, err = a.backend.getLogsByCursor(nextCursor)

		if err != nil {
			utils.RestErrorWrapper(w, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
			return
		}

		if result.NextCursor != "" {
			claims := CursorClaim{
				NextCursor: result.NextCursor,
				StandardClaims: jwtgo.StandardClaims{
					ExpiresAt: time.Now().Add(time.Duration(time.Minute * 2)).Unix(),
					IssuedAt:  time.Now().Unix(),
					Audience:  own.(string),
				},
			}
			token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm), claims)
			ss, err := token.SignedString(a.jwtMiddleware.Key)
			if err != nil {
				utils.RestErrorWrapper(w, "ERROR: signing scrollid token: "+err.Error(), http.StatusInternalServerError)
				return
			}
			result.NextCursor = ss
		}

		w.WriteJson(result)
		return
	}

	utils.RestErrorWrapper(w, "Unexpected Code", http.StatusInternalServerError)
	return
}

func unmarshalBody(body []byte) ([]Entry, error) {
	entries := make([]Entry, 1)

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
func (a *App) handlePostLogs(w rest.ResponseWriter, r *rest.Request) {

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Need to be logged in as DEVICE to post logs", http.StatusForbidden)
		return
	}

	device, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as device with owner", http.StatusForbidden)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		utils.RestErrorWrapper(w, "Error reading logs body", http.StatusBadRequest)
		return
	}

	entries, err := unmarshalBody(body)

	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing logs body: "+err.Error(), http.StatusBadRequest)
		return
	}

	newEntries := []Entry{}

	for _, v := range entries {
		v.ID, err = primitive.ObjectIDFromHex(bson.NewObjectId().Hex())
		if err != nil {
			utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
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
		utils.RestErrorWrapper(w, "Error posting logs "+err.Error(), http.StatusInternalServerError)
		log.Println("ERROR: Error posting logs " + err.Error())
		return
	}

	w.WriteJson(newEntries)
}

// New create a new logs rest application
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	var err error
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
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

	app.API = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/logs:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "logs"})

	app.API.Use(rest.DefaultCommonStack...)

	// we allow calls from other domains to allow webapps; XXX: review
	app.API.Use(&rest.CorsMiddleware{
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

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwtMiddleware,
	})

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/", app.handleGetLogs),
		rest.Get("/cursor", app.handleGetLogsCursor),
		rest.Post("/cursor", app.handleGetLogsCursor),
		rest.Post("/", app.handlePostLogs),
	)
	app.API.SetApp(apiRouter)

	return app
}
