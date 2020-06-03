//
// Copyright 2016-2020  Pantacor Ltd.
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

package objects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/subscriptions"

	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"

	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ErrNoBackingFile error signals that an object is not fully resolvable as
// it has no backing file yet.
var ErrNoBackingFile = errors.New("No backing file nor link")
var ErrNoLinkTargetAvail = errors.New("No link target available")

const (
	HttpHeaderPantahubObjectType = "Pantahub-Object-Type"
	ObjectTypeLink               = "link"
	ObjectTypeObject             = "object"
)

// App objects rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
	subService    subscriptions.SubscriptionService

	awsS3Bucket string
	awsRegion   string
}

// Build factory a new Object App  with mongoClient
func Build(mongoClient *mongo.Client) *App {

	adminUsers := utils.GetSubscriptionAdmins()
	subService := subscriptions.NewService(mongoClient, utils.Prn("prn::subscriptions:"), adminUsers, subscriptions.SubscriptionProperties)

	return &App{
		mongoClient: mongoClient,
		subService:  subService,
		awsS3Bucket: "systemcloud-001",
		awsRegion:   "us-east-1",
	}

}

var pantahubHTTPSURL string

func init() {
	pantahubHost := utils.GetEnv(utils.EnvPantahubHost)
	pantahubPort := utils.GetEnv(utils.EnvPantahubPort)
	pantahubScheme := utils.GetEnv(utils.EnvPantahubScheme)

	pantahubHTTPSURL = pantahubScheme + "://" + pantahubHost

	if pantahubPort != "" {
		pantahubHTTPSURL += ":" + pantahubPort
	}
}

// PantahubS3DevURL s3 dev url
func PantahubS3DevURL() string {
	return pantahubHTTPSURL
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// MakeStorageID crerate a new storage ID
func MakeStorageID(owner string, sha []byte) string {
	shaStr := hex.EncodeToString(sha)
	res := sha256.Sum256(append([]byte(owner + "/" + shaStr)))
	newSha := res[:]
	hexRes := make([]byte, hex.EncodedLen(len(newSha)))
	hex.Encode(hexRes, newSha)
	return string(hexRes)
}

// GetDiskQuota get disk quota for a object
func (a *App) GetDiskQuota(prn string) (float64, error) {

	sub, err := a.subService.LoadBySubject(utils.Prn(prn))
	if err != nil {
		sub = a.subService.GetDefaultSubscription(utils.Prn(prn))
	}

	quota := sub.GetProperty("OBJECTS").(string)

	uM, err := units.ParseStrictBytes(quota)
	if err != nil {
		return 0, err
	}

	return float64(uM), err
}

var defaultObjectsApp *App

// GetDiskQuota public function to get the default disk quota
func GetDiskQuota(prn string) (float64, error) {
	return defaultObjectsApp.GetDiskQuota(prn)
}

// SyncObjectSizes syncronize objects sizes
func SyncObjectSizes(obj *Object) {
	var err error
	var strInt64 int64

	// if string is not set we go fro the int regardless
	if obj.Size == "" {
		obj.Size = fmt.Sprintf("%d", obj.SizeInt)
		return
	}
	// now lets parse the string
	strInt64, err = strconv.ParseInt(obj.Size, 10, 64)

	// if we failed to parse it or if int value is set in object we use the int
	if err != nil || obj.SizeInt != 0 {
		obj.Size = fmt.Sprintf("%d", obj.SizeInt)
	} else {
		// all rest get the string variant
		obj.SizeInt = strInt64
	}
}

// MakeObjAccessible make a object accessible
func MakeObjAccessible(Issuer string, Subject string, obj Object, storageID string) ObjectWithAccess {
	filesObjWithAccess := ObjectWithAccess{}
	filesObjWithAccess.Object = obj

	timeNow := time.Now()
	filesObjWithAccess.Now = strconv.FormatInt(timeNow.Unix(), 10)
	filesObjWithAccess.ExpireTime = strconv.FormatInt(15, 10)

	size, err := strconv.ParseInt(obj.Size, 10, 64)
	if err != nil {
		log.Println("INTERNAL ERROR (size parsing) local-s3: " + err.Error())
		filesObjWithAccess.SignedGetURL = PantahubS3DevURL() + "/local-s3/INTERNAL-ERROR"
		filesObjWithAccess.SignedPutURL = PantahubS3DevURL() + "/local-s3/INTERNAL-ERROR"
		return filesObjWithAccess
	}

	// resolve a link if any...
	realStorageID := storageID
	if obj.LinkedObject != "" {
		realStorageID = obj.LinkedObject
	}

	objAccessTokGet := NewObjectAccessForSec(obj.ObjectName, http.MethodGet, size, filesObjWithAccess.Sha, Issuer,
		Subject, realStorageID, ObjectTokenValidSec)
	tokGet, err := objAccessTokGet.Sign()
	if err != nil {
		log.Println("INTERNAL ERROR local-s3: " + err.Error())
		filesObjWithAccess.SignedGetURL = PantahubS3DevURL() + "/local-s3/INTERNAL-ERROR"
	} else {
		filesObjWithAccess.SignedGetURL = PantahubS3DevURL() + "/local-s3/" + tokGet
	}

	// Put URLs only allowed when going for
	if Subject == obj.Owner {
		if obj.LinkedObject == "" {
			realStorageID = storageID
		} else {
			realStorageID = "SHAONLY"
		}
		objAccessTokPut := NewObjectAccessForSec(obj.ObjectName, http.MethodPut,
			size, filesObjWithAccess.Sha, Issuer, Subject, realStorageID, ObjectTokenValidSec)
		tokPut, err := objAccessTokPut.Sign()
		if err != nil {
			log.Println("INTERNAL ERROR local-s3: " + err.Error())
			filesObjWithAccess.SignedPutURL = PantahubS3DevURL() + "/local-s3/INTERNAL-ERROR"
		} else {
			filesObjWithAccess.SignedPutURL = PantahubS3DevURL() + "/local-s3/" + tokPut
		}
	}

	return filesObjWithAccess
}

// New create a new object rest application
func New(jwtMiddleware *jwt.JWTMiddleware, subService subscriptions.SubscriptionService,
	mongoClient *mongo.Client) *App {

	app := new(App)
	if defaultObjectsApp == nil {
		defaultObjectsApp = app
	}
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient
	app.subService = subService

	// XXX: allow config through env
	app.awsS3Bucket = "systemcloud-001"
	app.awsRegion = "us-east-1"

	// Indexing for the owner,garbage fields in pantahub_objects
	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_objects: " + err.Error())
		return nil
	}

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/objects:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "objects"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})

	app.API.Use(rest.DefaultCommonStack...)
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

	readObjectsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.Objects,
		utils.Scopes.ReadObjects,
	}
	writeObjectScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.Objects,
		utils.Scopes.WriteObjects,
	}

	// /auth_status endpoints
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/auth_status", utils.ScopeFilter(readObjectsScopes, handleAuth)),
		rest.Get("/", utils.ScopeFilter(readObjectsScopes, app.handleGetObjects)),
		rest.Post("/", utils.ScopeFilter(writeObjectScopes, app.handlePostObject)),
		rest.Get("/:id", utils.ScopeFilter(readObjectsScopes, app.handleGetObject)),
		rest.Get("/:id/blob", utils.ScopeFilter(readObjectsScopes, app.handleGetObjectFile)),
		rest.Put("/:id", utils.ScopeFilter(writeObjectScopes, app.handlePutObject)),
		rest.Delete("/:id", utils.ScopeFilter(writeObjectScopes, app.handleDeleteObject)),
	)
	app.API.SetApp(apiRouter)

	return app
}
