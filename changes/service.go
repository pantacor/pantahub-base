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

package changes

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const createIndexTimeout = 3000 * time.Second
const devicesLastModifiedKeyConst = "timemodified"
const stepsLastModifiedKeyConst = "timemodified"
const trailsLastModifiedKeyConst = "last-touched"

// App Web app structure
type App struct {
	jwtConfig        *jwtauth.Config
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
	return &trailmodels.Step{}
}

func findProtoTrails() interface{} {
	return &trailmodels.Trail{}
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

	var step *trailmodels.Step

	if proto == nil {
		return nil, errors.New("proto step is nil")
	}

	step = proto.(*trailmodels.Step)

	if step == nil {
		return nil, errors.New("proto not a valid device")
	}

	return &step.TimeModified, nil
}

func timeModifiedTrail(proto interface{}) (*time.Time, error) {

	var trail *trailmodels.Trail

	if proto == nil {
		return nil, errors.New("proto trail is nil")
	}

	trail = proto.(*trailmodels.Trail)

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
func (a *App) handleGetChangesDevices(c *echo.Context) error {
	return a.handleGetChangesGeneric(c, "/changes/devices", a.deviceCollection, findProtoDevices,
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
func (a *App) handleGetChangesSteps(c *echo.Context) error {
	return a.handleGetChangesGeneric(c, "/changes/steps", a.stepCollection, findProtoSteps,
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
func (a *App) handleGetChangesTrail(c *echo.Context) error {
	return a.handleGetChangesGeneric(c, "/changes/trails", a.trailCollection, findProtoTrails, trailsLastModifiedKeyConst, timeModifiedTrail)
}

func (a *App) handleGetChangesGeneric(c *echo.Context, basePath string,
	col *mongo.Collection, findProtoFunc FindPrototypeFunc, timeModifiedKey string, timeModifiedFunc TimeModfiedFunc) error {

	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD", http.StatusBadRequest)
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
	}

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
	}

	collection := col
	res := ChangePage{}

	pageSizeS := c.Request().URL.Query().Get("page[size]")
	pageAfter := c.Request().URL.Query().Get("page[after]")
	pageBefore := c.Request().URL.Query().Get("page[before]")

	if pageSizeS == "" {
		pageSizeS = "50"
		return nil
	}

	pageSize, err := strconv.ParseInt(pageSizeS, 10, 64)

	if err != nil {
		return echoutil.RestErrorUser(c, err, "page[size] must be valid integer", http.StatusBadRequest)
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
			return echoutil.RestErrorUser(c, err, "Error parsing time format", http.StatusBadRequest)
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

	cur, err := collection.Find(c.Request().Context(), q, findOptions)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "error getting changes for user:"+err.Error(), http.StatusForbidden)
	}

	var links ChangePageCursor
	data := []interface{}{}

	linkBase := basePath + "?page[size]=" + pageSizeS + "&"
	links.Next = ""
	links.Prev = ""

	var firstDone bool
	var result interface{}
	var tm *time.Time

	defer cur.Close(c.Request().Context())
	for cur.Next(c.Request().Context()) {
		result = findProtoFunc()
		err := cur.Decode(result)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
		}
		if !firstDone {
			tm, err = timeModifiedFunc(result)
			if err != nil {
				return echoutil.RestErrorWrapper(c, "Internal error extracting time modifed: "+err.Error(), http.StatusForbidden)
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
			return echoutil.RestErrorWrapper(c, "Internal error extracting time modifed: "+err.Error(), http.StatusForbidden)
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

	return echoutil.WriteJSON(c, http.StatusOK, res)
}

// New create devices web app
func New(jwtConfig *jwtauth.Config, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtConfig = jwtConfig
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
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: devicesLastModifiedKeyConst, Value: int32(1)},
		},
		Options: &indexOptionDevices,
	}
	_, err := app.deviceCollection.Indexes().CreateOne(context.Background(), index1, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	index2 := mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: stepsLastModifiedKeyConst, Value: int32(1)},
		},
		Options: &indexOptionsSteps,
	}

	_, err = app.stepCollection.Indexes().CreateOne(context.Background(), index2, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	index3 := mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: trailsLastModifiedKeyConst, Value: int32(1)},
		},
		Options: &indexOptionsTrails,
	}

	_, err = app.trailCollection.Indexes().CreateOne(context.Background(), index3, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_trails: " + err.Error())
		return nil
	}

	return app
}

// Mount registers changes on the echo server.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/changes"

	readDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	}
	readTrailsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.ReadTrails,
	}

	readStepsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.ReadTrails,
	}

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/changes:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "changes"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests:         false,
			OriginValidator:               echoutil.AllowAllOrigins,
			AllowedMethods:                []string{"GET"},
			AllowedHeaders:                []string{"*"},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.JWT(app.jwtConfig),
		echoutil.Auth(),
	)

	g.GET("/devices", echoutil.ScopeFilter(readDevicesScopes, app.handleGetChangesDevices))
	g.GET("/steps", echoutil.ScopeFilter(readStepsScopes, app.handleGetChangesSteps))
	g.GET("/trails", echoutil.ScopeFilter(readTrailsScopes, app.handleGetChangesTrail))
}
