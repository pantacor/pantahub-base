//
// Copyright 2026 Pantacor Ltd.
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
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "gitlab.com/pantacor/pantahub-base/utils/jwtmiddleware"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	LogRev      string             `json:"rev,omitempty" bson:"rev"`
	LogPlat     string             `json:"plat,omitempty" bson:"plat"`
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
	// getLogs runs one page of a log query. searchAfter, when non-empty,
	// carries the sort values of the last entry of the previous page and
	// continues from just after it (keyset pagination); it is mutually
	// exclusive with a non-zero start. When cursor is true the backend fills
	// Pager.NextCursor with the sort values needed to fetch the next page,
	// or leaves it empty when the backend cannot paginate that way.
	getLogs(ctx context.Context, start int64, page int64, before *time.Time, after *time.Time,
		query Filters, sort Sorts, searchAfter []interface{}, cursor bool) (*Pager, error)
	postLogs(parentCtx context.Context, e []Entry, debug bool) error
	register() error
	unregister(deleteIndices bool) error
}

// ErrCursorTimedOut invalid cursor error
var ErrCursorTimedOut error = errors.New("cursor Invalid or expired")

// ErrCursorNotImplemented cursor not implemented
var ErrCursorNotImplemented error = errors.New("cursor not supported by backend")

// CursorState is everything needed to continue a log query, so that paging
// holds no server-side state at all. Previously the cursor was an
// Elasticsearch scroll id, which pinned a scroll context on the cluster for
// every request that asked for one and was never released; callers that only
// ever fetch the first page (the UI's poller did exactly this) leaked one
// context per poll. Carrying the query plus the previous page's sort values
// instead lets the next page be re-issued as a plain search_after query.
type CursorState struct {
	Filter      Entry         `json:"f"`
	Before      *time.Time    `json:"b,omitempty"`
	After       *time.Time    `json:"a,omitempty"`
	Sort        Sorts         `json:"s,omitempty"`
	Page        int64         `json:"p,omitempty"`
	SearchAfter []interface{} `json:"sa,omitempty"`
}

// CursorClaim claim log cursor
type CursorClaim struct {
	State *CursorState `json:"state,omitempty"`
	jwtgo.StandardClaims
}

// ParseDeviceString : Parse Device Nicks & Device Id's from a string and replace them with device Prn
func (a *App) ParseDeviceString(parentCtx context.Context, owner string, devicesString string) (string, error) {
	// No device filter asked for: nothing to resolve, and no reason to spend a
	// round trip on the devices collection.
	if strings.TrimSpace(devicesString) == "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return "", errors.New("Error with Database connectivity")
	}
	devicePrns := []string{}

	components := strings.Split(devicesString, ",")
	deviceObject := devices.Device{}
	for _, device := range components {
		device = strings.TrimSpace(device)
		if device == "" {
			continue
		}

		hasPrefix, _ := regexp.MatchString("^prn:(.*):devices:/(.+)$", device)
		if hasPrefix {
			devicePrns = append(devicePrns, device)
			continue
		}

		query := bson.M{
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}

		isPrefix := false
		if deviceObjectID, err := primitive.ObjectIDFromHex(device); err == nil {
			query["_id"] = deviceObjectID
		} else if low, high, ok := objectIDPrefixRange(device); ok {
			// A shortened id, as `pvr device logs 648b56a6` passes. An ObjectID
			// hex prefix denotes a contiguous range of ids, so this stays an
			// indexed lookup rather than a scan.
			query["_id"] = bson.M{"$gte": low, "$lte": high}
			isPrefix = true
		} else {
			query["nick"] = device
		}

		if isPrefix {
			// The leading bytes of an ObjectID are a timestamp, so a short
			// prefix can name several devices registered in the same second.
			// Picking one of them arbitrarily would show the wrong device's
			// logs without saying so.
			prn, err := resolveUniqueDevice(ctx, collection, query)
			if err == nil {
				devicePrns = append(devicePrns, prn)
				continue
			}
			if !errors.Is(err, errNoSuchDevice) {
				return "", fmt.Errorf("device %q: %w", device, err)
			}

			// Nothing has an id starting that way, but a nick can be valid hex
			// too, so fall through and try it as one before giving up.
			delete(query, "_id")
			query["nick"] = device
		}

		err := collection.FindOne(ctx, query).Decode(&deviceObject)
		if err != nil {
			// Returning nothing for an unresolved device used to leave the
			// filter empty, and an empty filter means "every device" -- so
			// asking for one device you could not name quietly returned all of
			// them. Say so instead.
			return "", fmt.Errorf("no device matching %q", device)
		}

		if deviceObject.Prn != "" {
			devicePrns = append(devicePrns, deviceObject.Prn)
		}
	}

	if len(devicePrns) == 0 {
		return "", errors.New("no devices matched the requested filter")
	}

	return strings.Join(devicePrns, ","), nil
}

// errNoSuchDevice reports that a query matched nothing, so a caller can decide
// whether another interpretation of the name is worth trying.
var errNoSuchDevice = errors.New("no such device")

// resolveUniqueDevice returns the PRN of the single device matching query, and
// refuses when the match is ambiguous rather than picking one arbitrarily. It
// reads two documents so that "more than one" costs no more than "exactly one".
func resolveUniqueDevice(ctx context.Context, collection *mongo.Collection, query bson.M) (string, error) {
	cursor, err := collection.Find(ctx, query, options.Find().SetLimit(2))
	if err != nil {
		return "", err
	}
	defer func() { _ = cursor.Close(ctx) }()

	matches := []devices.Device{}
	for cursor.Next(ctx) {
		match := devices.Device{}
		if err := cursor.Decode(&match); err != nil {
			return "", err
		}
		matches = append(matches, match)
	}
	if err := cursor.Err(); err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", errNoSuchDevice
	case 1:
		if matches[0].Prn == "" {
			return "", errors.New("device has no prn")
		}
		return matches[0].Prn, nil
	default:
		return "", errors.New("matches more than one device; use the full id")
	}
}

// objectIDPrefixRange turns a partial ObjectID hex string into the inclusive
// range of ids that begin with it.
//
// An ObjectID is 12 bytes rendered as 24 hex characters, so any prefix of
// those characters describes a contiguous span: pad it with zeroes for the low
// end and with fs for the high end. That keeps a lookup by shortened id on the
// _id index instead of forcing a collection scan with a regex over $toString.
//
// Reports false when the string is not a usable prefix, so the caller can fall
// back to treating it as a nick.
func objectIDPrefixRange(prefix string) (primitive.ObjectID, primitive.ObjectID, bool) {
	var low, high primitive.ObjectID

	// A full-length id is handled by the exact-match path, and an odd number of
	// characters would not land on a byte boundary when padded.
	if len(prefix) == 0 || len(prefix) >= 24 || len(prefix)%2 != 0 {
		return low, high, false
	}

	for _, r := range prefix {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return low, high, false
		}
	}

	padding := 24 - len(prefix)
	low, err := primitive.ObjectIDFromHex(prefix + strings.Repeat("0", padding))
	if err != nil {
		return low, high, false
	}
	high, err = primitive.ObjectIDFromHex(prefix + strings.Repeat("f", padding))
	if err != nil {
		return low, high, false
	}

	return low, high, true
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

// New create a new logs rest application
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	var err error
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	loggerType := "elastic"

	app.backend, err = NewElasticLogger()

	if err != nil {
		log.Fatalf("INFO: Elastic Logger failed to start '%s'; misconfiguration -> exiting\n", err.Error())
	}

	if app.backend == nil {
		log.Println("INFO: Elastic Logger not configured; trying other options ...")

		app.backend, err = NewMgoLogger(mongoClient)
		loggerType = "mongo"
		if err != nil {
			log.Fatalf("INFO: Mongo Logger failed to start '%s'; misconfiguration -> exiting\n", err.Error())
		}
	}

	err = app.backend.register()
	if err != nil {
		log.Fatalf("INFO: Logger failed to register '%s'; misconfiguration -> exiting\n", err.Error())
	}

	log.Printf("INFO: %s Logger started\n", loggerType)

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
			"Accept",
			"Content-Type",
			"Content-Length",
			"X-Custom-Header",
			"Origin",
			"Authorization",
			"X-Trace-ID",
			"Trace-Id",
			"x-request-id",
			"X-Request-ID",
			"TraceID",
			"ParentID",
			"Uber-Trace-ID",
			"uber-trace-id",
			"traceparent",
			"tracestate",
		},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	app.API.Use(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtMiddleware, Mongo: app.mongoClient})
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
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
}
