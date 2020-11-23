//
// Copyright 2020  Pantacor Ltd.
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

package changes

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
	"gopkg.in/mgo.v2/bson"
)

const createIndexTimeout = 3000 * time.Second
const devicesLastModifiedKeyConst = "timemodified"
const stepsLastModifiedKeyConst = "timemodified"
const trailsLastModifiedKeyConst = "last-touched"

// App Web app structure
type App struct {
	jwtMiddleware    *jwt.JWTMiddleware
	API              *rest.Api
	mongoClient      *mongo.Client
	deviceCollection *mongo.Collection
	stepCollection   *mongo.Collection
	trailCollection  *mongo.Collection
}

// Pagination: https://jsonapi.org/profiles/ethanresnick/cursor-pagination/
type ChangePageCursor struct {
	Next string `json:"next"`
	Prev string `json:"prev"`
}

type ChangePage struct {
	Links *ChangePageCursor `json:"links"`
	Data  []interface{}     `json:"data"`
}

type FindPrototypeFunc = func() interface{}

func findProtoSteps() interface{} {
	return &trails.Step{}
}

func findProtoTrails() interface{} {
	return &trails.Trail{}
}

func findProtoDevices() interface{} {
	return &devices.Device{}
}

type TimeModfiedFunc = func(proto interface{}) (*time.Time, error)

func timeModifiedDevice(proto interface{}) (*time.Time, error) {

	var dev *devices.Device

	if proto == nil {
		return nil, errors.New("proto device is nil")
	}

	dev = proto.(*devices.Device)

	if dev == nil {
		return nil, errors.New("proto not a valid device")
	}

	return &dev.TimeModified, nil
}

func timeModifiedStep(proto interface{}) (*time.Time, error) {

	var step *trails.Step

	if proto == nil {
		return nil, errors.New("proto step is nil")
	}

	step = proto.(*trails.Step)

	if step == nil {
		return nil, errors.New("proto not a valid device")
	}

	return &step.TimeModified, nil
}

func timeModifiedTrail(proto interface{}) (*time.Time, error) {

	var trail *trails.Trail

	if proto == nil {
		return nil, errors.New("proto trail is nil")
	}

	trail = proto.(*trails.Trail)

	if trail == nil {
		return nil, errors.New("proto not a valid device")
	}

	return &trail.LastTouched, nil
}

// handleGetChangesDevices Get all changes to devices
// @Summary Get all devices that have changed after or before a given point in time
// @Description Get all devices after or before the cursor passed in as argument.
// @Description Result will be sorted inverse order for before and natural order for after
// @Description The page[after] flag takes precedence in case it is provided with page[before]
// @Description Tries to follow https://jsonapi.org/profiles/ethanresnick/cursor-pagination/
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {object} ChangePage
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /changes/devices [get]
func (a *App) handleGetChangesDevices(w rest.ResponseWriter, r *rest.Request) {
	a.handleGetChangesGeneric(w, r, "/changes/devices", a.deviceCollection, findProtoDevices,
		devicesLastModifiedKeyConst, timeModifiedDevice)
}

// handleGetChangesSteps Get all changes to steps
// @Summary Get all steps that have changed after or before a given point in time
// @Description Get all steps after or before a cursor passed in as argument.
// @Description following https://jsonapi.org/profiles/ethanresnick/cursor-pagination/
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {object} ChangePage
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /changes/steps [get]
func (a *App) handleGetChangesSteps(w rest.ResponseWriter, r *rest.Request) {
	a.handleGetChangesGeneric(w, r, "/changes/steps", a.stepCollection, findProtoSteps,
		stepsLastModifiedKeyConst, timeModifiedStep)
}

// handleGetChangesDevices Get all changes to devices
// @Summary Get all devices that have changed after or before a given point in time
// @Description Get all devices after or before the cursor passed in as argument.
// @Description Result will be sorted inverse order for before and natural order for after
// @Description The page[after] flag takes precedence in case it is provided with page[before]
// @Description Tries to follow https://jsonapi.org/profiles/ethanresnick/cursor-pagination/
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Success 200 {object} ChangePage
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /changes/trails [get]
func (a *App) handleGetChangesTrail(w rest.ResponseWriter, r *rest.Request) {
	a.handleGetChangesGeneric(w, r, "/changes/trails", a.trailCollection, findProtoTrails, trailsLastModifiedKeyConst, timeModifiedTrail)
}

func (a *App) handleGetChangesGeneric(w rest.ResponseWriter, r *rest.Request, basePath string,
	col *mongo.Collection, findProtoFunc FindPrototypeFunc, timeModifiedKey string, timeModifiedFunc TimeModfiedFunc) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
		return
	}

	if authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	collection := col
	res := ChangePage{}

	pageSizeS := r.URL.Query().Get("page[size]")
	pageAfter := r.URL.Query().Get("page[after]")
	pageBefore := r.URL.Query().Get("page[before]")

	if pageSizeS == "" {
		pageSizeS = "50"
		return
	}

	pageSize, err := strconv.ParseInt(pageSizeS, 10, 64)

	if err != nil {
		utils.RestErrorUser(w, err, "page[size] must be valid integer", http.StatusBadRequest)
		return
	}

	if pageSize > 250 {
		pageSize = 250
	}

	pageTimeS := ""
	isAfter := true

	if pageAfter != "" {
		pageTimeS = pageAfter
	} else if pageBefore != "" {
		pageTimeS = pageBefore
		isAfter = false
	}

	q := bson.M{
		"owner": caller.(string),
	}

	var pageTime time.Time

	if pageTimeS == "" {
		pageTime = time.Now()
	} else {
		pageTime, err = time.Parse(time.RFC3339Nano, pageTimeS)
		if err != nil {
			utils.RestErrorUser(w, err, "Error parsing time format", http.StatusBadRequest)
			return
		}
	}

	if isAfter {
		q[timeModifiedKey] = bson.M{
			"$gt": pageTime,
		}
	} else {
		q[timeModifiedKey] = bson.M{
			"$lt": pageTime,
		}
	}

	findOptions := options.Find().SetLimit(pageSize)
	if isAfter {
		findOptions = findOptions.SetSort(bson.M{timeModifiedKey: 1})
	} else {
		findOptions = findOptions.SetSort(bson.M{timeModifiedKey: -1})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, q, findOptions)

	if err != nil {
		utils.RestErrorWrapper(w, "error getting changes for user:"+err.Error(), http.StatusForbidden)
		return
	}

	var links ChangePageCursor
	data := []interface{}{}

	linkBase := basePath + "?page[size]=" + pageSizeS + "&"
	links.Next = ""
	links.Prev = ""

	var firstDone bool
	var result interface{}
	var tm *time.Time

	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result = findProtoFunc()
		err := cur.Decode(result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		if !firstDone {
			tm, err = timeModifiedFunc(result)
			if err != nil {
				utils.RestErrorWrapper(w, "Internal error extracting time modifed: "+err.Error(), http.StatusForbidden)
				return
			}
			if isAfter {
				links.Prev = utils.GetAPIEndpoint(linkBase + "page[before]=" + tm.Format(time.RFC3339Nano))
			} else {
				links.Next = utils.GetAPIEndpoint(linkBase + "page[after]=" + tm.Format(time.RFC3339Nano))
			}
			firstDone = true
		}

		// prepend to achieve natural sort order for the data part in
		// both before and after case.
		if isAfter {
			data = append(data, result)
		} else {
			data = append([]interface{}{result}, data...)
		}
	}

	if result != nil {
		tm, err = timeModifiedFunc(result)
		if err != nil {
			utils.RestErrorWrapper(w, "Internal error extracting time modifed: "+err.Error(), http.StatusForbidden)
			return
		}
	}

	if tm == nil {
		t := time.Now()
		tm = &t
	}

	if isAfter {
		if result == nil {
			links.Prev = utils.GetAPIEndpoint(linkBase + "page[before]=" + tm.Format(time.RFC3339Nano))
			firstDone = true
		} else {
			links.Next = utils.GetAPIEndpoint(linkBase + "page[after]=" + tm.Format(time.RFC3339Nano))
			firstDone = true
		}
	} else if !isAfter {
		if result == nil {
			links.Next = utils.GetAPIEndpoint(linkBase + "page[after]=" + tm.Format(time.RFC3339Nano))
			firstDone = true
		} else {
			links.Prev = utils.GetAPIEndpoint(linkBase + "page[before]=" + tm.Format(time.RFC3339Nano))
			firstDone = true
		}
	}

	res.Links = &links
	res.Data = data

	w.WriteJson(res)
}

// New create devices web app
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	app.deviceCollection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	app.stepCollection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	app.trailCollection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(createIndexTimeout)

	indexOptionDevices := options.IndexOptions{}
	indexOptionDevices.SetUnique(false)
	indexOptionDevices.SetSparse(false)
	indexOptionDevices.SetBackground(true)

	indexOptionsSteps := indexOptionDevices
	indexOptionsTrails := indexOptionDevices

	index1 := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: devicesLastModifiedKeyConst, Value: bsonx.Int32(1)},
		},
		Options: &indexOptionDevices,
	}
	_, err := app.deviceCollection.Indexes().CreateOne(context.Background(), index1, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	index2 := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: stepsLastModifiedKeyConst, Value: bsonx.Int32(1)},
		},
		Options: &indexOptionsSteps,
	}

	_, err = app.stepCollection.Indexes().CreateOne(context.Background(), index2, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	index3 := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: trailsLastModifiedKeyConst, Value: bsonx.Int32(1)},
		},
		Options: &indexOptionsTrails,
	}

	_, err = app.trailCollection.Indexes().CreateOne(context.Background(), index3, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_trails: " + err.Error())
		return nil
	}

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/changes:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "changes"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})

	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods:                []string{"GET"},
		AllowedHeaders:                []string{"*"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	app.API.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// if call is coming with authorization attempt, ensure JWT middleware
			// is used... otherwise let through anonymous POST for registration
			auth := request.Header.Get("Authorization")
			if auth != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth)), "bearer ") {
				return true
			}

			// not authorized is a fail...
			return false
		},
		IfTrue: app.jwtMiddleware,
	})

	readDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	}

	// /auth_status endpoints
	apiRouter, _ := rest.MakeRouter(
		// TPM auto enroll register
		rest.Get("/devices", utils.ScopeFilter(readDevicesScopes, app.handleGetChangesDevices)),
		rest.Get("/steps", utils.ScopeFilter(readDevicesScopes, app.handleGetChangesSteps)),
		rest.Get("/trails", utils.ScopeFilter(readDevicesScopes, app.handleGetChangesTrail)),
	)

	app.API.SetApp(apiRouter)

	return app
}
