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
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"

	"github.com/labstack/echo/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/alecthomas/units"
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
	jwtConfig   *jwtauth.Config
	mongoClient *mongo.Client
	subService  subscriptions.SubscriptionService

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

func handleAuth(c *echo.Context) error {
	jwtClaims := c.Get(echoutil.KeyJWTPayload)
	return echoutil.WriteJSON(c, http.StatusOK, jwtClaims)
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
func (a *App) GetDiskQuota(pctx context.Context, prn string) (float64, error) {

	sub, err := a.subService.LoadBySubject(pctx, utils.Prn(prn))
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
func GetDiskQuota(ctx context.Context, prn string) (float64, error) {
	return defaultObjectsApp.GetDiskQuota(ctx, prn)
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
func New(jwtConfig *jwtauth.Config, subService subscriptions.SubscriptionService,
	mongoClient *mongo.Client) *App {

	app := new(App)
	if defaultObjectsApp == nil {
		defaultObjectsApp = app
	}
	app.jwtConfig = jwtConfig
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
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_objects: " + err.Error())
		return nil
	}

	return app
}

// Mount registers objects on echo with its previous middleware stack.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/objects"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/objects:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "objects"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
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
		}),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: app.jwtConfig, Mongo: app.mongoClient}),
		echoutil.JWT(app.jwtConfig),
		echoutil.Auth(),
	)

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

	g.GET("/auth_status", echoutil.ScopeFilter(readObjectsScopes, handleAuth))
	g.GET("/", echoutil.ScopeFilter(readObjectsScopes, app.handleGetObjects))
	g.POST("/", echoutil.ScopeFilter(writeObjectScopes, app.handlePostObject))
	g.GET("/:id", echoutil.ScopeFilter(readObjectsScopes, app.handleGetObject))
	g.GET("/:id/blob", echoutil.ScopeFilter(readObjectsScopes, app.handleGetObjectFile))
	g.PUT("/:id", echoutil.ScopeFilter(writeObjectScopes, app.handlePutObject))
	g.DELETE("/:id", echoutil.ScopeFilter(writeObjectScopes, app.handleDeleteObject))
}
