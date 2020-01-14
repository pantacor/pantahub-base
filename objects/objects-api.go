//
// Copyright 2016-2018  Pantacor Ltd.
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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/storagedriver"
	"gitlab.com/pantacor/pantahub-base/subscriptions"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"

	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2/bson"
)

type ObjectsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mongoClient    *mongo.Client
	subService     subscriptions.SubscriptionService

	awsS3Bucket string
	awsRegion   string
}

var pantahubHttpsUrl string

func init() {
	pantahubHost := utils.GetEnv(utils.ENV_PANTAHUB_HOST)
	pantahubPort := utils.GetEnv(utils.ENV_PANTAHUB_PORT)
	pantahubScheme := utils.GetEnv(utils.ENV_PANTAHUB_SCHEME)

	pantahubHttpsUrl = pantahubScheme + "://" + pantahubHost

	if pantahubPort != "" {
		pantahubHttpsUrl += ":" + pantahubPort
	}
}

func PantahubS3DevUrl() string {
	return pantahubHttpsUrl
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func MakeStorageId(owner string, sha []byte) string {
	shaStr := hex.EncodeToString(sha)
	res := sha256.Sum256(append([]byte(owner + "/" + shaStr)))
	newSha := res[:]
	hexRes := make([]byte, hex.EncodedLen(len(newSha)))
	hex.Encode(hexRes, newSha)
	return string(hexRes)
}

func (a *ObjectsApp) handle_postobject(w rest.ResponseWriter, r *rest.Request) {

	newObject := Object{}

	r.DecodeJsonPayload(&newObject)

	var ownerStr string

	caller, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}
	callerStr, ok := caller.(string)

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if authType.(string) == "USER" {
		ownerStr = callerStr
	} else {
		owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
		if !ok {
			// XXX: find right error
			utils.RestErrorWrapper(w, "You need to be logged in as a USER or DEVICE", http.StatusForbidden)
			return
		}
		ownerStr = owner.(string)
	}

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access Token", http.StatusForbidden)
		return
	}

	newObject.Owner = ownerStr

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	// check preconditions
	var sha []byte
	var err error

	if newObject.Sha == "" {
		utils.RestErrorWrapper(w, "Post New Object must set a sha", http.StatusBadRequest)
		return
	}

	sha, err = utils.DecodeSha256HexString(newObject.Sha)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageId := MakeStorageId(ownerStr, sha)
	newObject.StorageId = storageId
	newObject.Id = newObject.Sha

	SyncObjectSizes(&newObject)

	result, err := CalcUsageAfterPost(ownerStr, a.mongoClient, newObject.Id, newObject.SizeInt)

	if err != nil {
		log.Printf("ERROR: CalcUsageAfterPost failed: %s\n", err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := a.GetDiskQuota(ownerStr)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {

		log.Println("Quota exceeded in post object.")
		utils.RestErrorWrapper(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newObject,
	)

	if err != nil {
		filePath, err := utils.MakeLocalS3PathForName(storageId)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error Finding Path for Name"+err.Error())
			return
		}

		sd := storagedriver.FromEnv()
		if sd.Exists(filePath) {
			w.WriteHeader(http.StatusConflict)
			w.Header().Add("X-PH-Error", "Cannot insert existing object into database")
			goto conflict
		}

		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		updatedResult, err := collection.UpdateOne(
			ctx,
			bson.M{"_id": newObject.StorageId},
			bson.M{"$set": newObject},
		)
		if updatedResult.MatchedCount == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error updating previously failed object in database ")
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error updating previously failed object in database "+err.Error())
			return
		}
		// we return anyway with the already available info about this object
	}
conflict:

	issuerUrl := utils.GetApiEndpoint("/objects")
	newObjectWithAccess := MakeObjAccessible(issuerUrl, ownerStr, newObject, storageId)
	w.WriteJson(newObjectWithAccess)
}

func (a *ObjectsApp) handle_putobject(w rest.ResponseWriter, r *rest.Request) {

	newObject := Object{}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
		return
	}
	putId := r.PathParam("id")

	sha, err := utils.DecodeSha256HexString(putId)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageId := MakeStorageId(ownerStr, sha)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageId,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner != owner {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newObject)

	newObject.Owner = owner.(string)
	newObject.StorageId = storageId
	newObject.Id = putId

	SyncObjectSizes(&newObject)
	result, err := CalcUsageAfterPut(ownerStr, a.mongoClient, newObject.Id, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := a.GetDiskQuota(ownerStr)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		utils.RestErrorWrapper(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		utils.RestErrorWrapper(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": storageId},
		bson.M{"$set": newObject},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newObject)
}

func (a *ObjectsApp) GetDiskQuota(prn string) (float64, error) {

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

var defaultObjectsApp *ObjectsApp

func GetDiskQuota(prn string) (float64, error) {
	return defaultObjectsApp.GetDiskQuota(prn)
}

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

func MakeObjAccessible(Issuer string, Subject string, obj Object, storageId string) ObjectWithAccess {
	filesObjWithAccess := ObjectWithAccess{}
	filesObjWithAccess.Object = obj

	timeNow := time.Now()
	filesObjWithAccess.Now = strconv.FormatInt(timeNow.Unix(), 10)
	filesObjWithAccess.ExpireTime = strconv.FormatInt(15, 10)

	size, err := strconv.ParseInt(obj.Size, 10, 64)
	if err != nil {
		log.Println("INTERNAL ERROR (size parsing) local-s3: " + err.Error())
		filesObjWithAccess.SignedGetUrl = PantahubS3DevUrl() + "/local-s3/INTERNAL-ERROR"
		filesObjWithAccess.SignedPutUrl = PantahubS3DevUrl() + "/local-s3/INTERNAL-ERROR"
		return filesObjWithAccess
	}

	objAccessTokGet := NewObjectAccessForSec(obj.ObjectName, http.MethodGet, size, filesObjWithAccess.Sha, Issuer, Subject, storageId, OBJECT_TOKEN_VALID_SEC)
	tokGet, err := objAccessTokGet.Sign()
	if err != nil {
		log.Println("INTERNAL ERROR local-s3: " + err.Error())
		filesObjWithAccess.SignedGetUrl = PantahubS3DevUrl() + "/local-s3/INTERNAL-ERROR"
	} else {
		filesObjWithAccess.SignedGetUrl = PantahubS3DevUrl() + "/local-s3/" + tokGet
	}

	if Subject == obj.Owner {
		objAccessTokPut := NewObjectAccessForSec(obj.ObjectName, http.MethodPut, size, filesObjWithAccess.Sha, Issuer, Subject, storageId, OBJECT_TOKEN_VALID_SEC)
		tokPut, err := objAccessTokPut.Sign()
		if err != nil {
			log.Println("INTERNAL ERROR local-s3: " + err.Error())
			filesObjWithAccess.SignedPutUrl = PantahubS3DevUrl() + "/local-s3/INTERNAL-ERROR"
		} else {
			filesObjWithAccess.SignedPutUrl = PantahubS3DevUrl() + "/local-s3/" + tokPut
		}
	}
	return filesObjWithAccess
}

func (a *ObjectsApp) handle_getobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
		// XXX: find right error
		if !ok {
			utils.RestErrorWrapper(w, "You need to be logged in as USER or DEVICE with owner", http.StatusForbidden)
			return
		}
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
		return
	}

	objId := r.PathParam("id")
	sha, err := utils.DecodeSha256HexString(objId)

	if err != nil {
		utils.RestErrorWrapper(w, "Get New Object :id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageId := MakeStorageId(ownerStr, sha)

	var filesObj Object
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageId,
		"garbage": bson.M{"$ne": true},
	}).Decode(&filesObj)

	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if filesObj.Owner != owner {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	issuerUrl := utils.GetApiEndpoint("/objects")
	filesObjWithAccess := MakeObjAccessible(issuerUrl, ownerStr, filesObj, storageId)

	w.WriteJson(filesObjWithAccess)
}

func (a *ObjectsApp) handle_getobjectfile(w rest.ResponseWriter, r *rest.Request) {

	// XXX: refactor: dupe code with getobject with getobject
	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
		// XXX: find right error
		if !ok {
			utils.RestErrorWrapper(w, "You need to be logged in as USER or DEVICE with owner", http.StatusForbidden)
			return
		}
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
		return
	}

	objId := r.PathParam("id")
	sha, err := utils.DecodeSha256HexString(objId)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}
	storageId := MakeStorageId(ownerStr, sha)

	var filesObj Object
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageId,
		"garbage": bson.M{"$ne": true},
	}).Decode(&filesObj)

	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if filesObj.Owner != owner {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	issuerUrl := utils.GetApiEndpoint("/objects")
	filesObjWithAccess := MakeObjAccessible(issuerUrl, ownerStr, filesObj, storageId)

	url := filesObjWithAccess.SignedGetUrl

	w.Header().Add("Location", url)
	w.WriteHeader(http.StatusFound)
}

func (a *ObjectsApp) handle_getobjects(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	filter := r.URL.Query().Get("filter")
	m := map[string]interface{}{}

	if filter != "" {
		err := json.Unmarshal([]byte(filter), &m)
		if err != nil {
			utils.RestErrorWrapper(w, "Error parsing filter json "+err.Error(), http.StatusInternalServerError)
		}
	}
	m["owner"] = owner
	m["garbage"] = bson.M{"$ne": true}

	newObjects := make([]Object, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching objects:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Object{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		newObjects = append(newObjects, result)
	}

	w.WriteJson(newObjects)
}

func (a *ObjectsApp) handle_deleteobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "Invalid Access", http.StatusForbidden)
		return
	}

	delId := r.PathParam("id")
	sha, err := utils.DecodeSha256HexString(delId)

	if err != nil {
		utils.RestErrorWrapper(w, "Post New Object sha must be a valid sha256", http.StatusBadRequest)
		return
	}
	storageId := MakeStorageId(ownerStr, sha)

	newObject := Object{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageId,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner == owner {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		deleteResult, err := collection.DeleteOne(ctx, bson.M{
			"_id":     storageId,
			"garbage": bson.M{"$ne": true},
		})
		if err != nil {
			utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
			return
		}
		if deleteResult.DeletedCount == 0 {
			utils.RestErrorWrapper(w, "Object not deleted", http.StatusForbidden)
			return
		}
	}

	w.WriteJson(newObject)
}

func New(jwtMiddleware *jwt.JWTMiddleware, subService subscriptions.SubscriptionService,
	mongoClient *mongo.Client) *ObjectsApp {

	app := new(ObjectsApp)
	if defaultObjectsApp == nil {
		defaultObjectsApp = app
	}
	app.jwt_middleware = jwtMiddleware
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

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/objects:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "objects"})
	app.Api.Use(&rest.StatusMiddleware{})
	app.Api.Use(&rest.TimerMiddleware{})
	app.Api.Use(&metrics.MetricsMiddleware{})

	app.Api.Use(rest.DefaultCommonStack...)
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
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", utils.ScopeFilter(readObjectsScopes, handle_auth)),
		rest.Get("/", utils.ScopeFilter(readObjectsScopes, app.handle_getobjects)),
		rest.Post("/", utils.ScopeFilter(writeObjectScopes, app.handle_postobject)),
		rest.Get("/:id", utils.ScopeFilter(readObjectsScopes, app.handle_getobject)),
		rest.Get("/:id/blob", utils.ScopeFilter(readObjectsScopes, app.handle_getobjectfile)),
		rest.Put("/:id", utils.ScopeFilter(writeObjectScopes, app.handle_putobject)),
		rest.Delete("/:id", utils.ScopeFilter(writeObjectScopes, app.handle_deleteobject)),
	)
	app.Api.SetApp(api_router)

	return app
}
