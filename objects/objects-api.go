//
// Copyright 2016,2017  Pantacor Ltd.
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/alecthomas/units"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type ObjectsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
	awsS3Bucket    string
	awsRegion      string
}

var pantahubS3Path string
var pantahubS3Production bool
var pantahubHttpsUrl string

func init() {
	pantahubS3Path = utils.GetEnv(utils.ENV_PANTAHUB_S3PATH)

	if pantahubS3Path == "production" {
		pantahubS3Production = true
	} else {
		pantahubS3Production = false
	}

	if pantahubS3Path == "" {
		pantahubS3Path = "../local-s3/"
	}

	pantahubHost := utils.GetEnv(utils.ENV_PANTAHUB_HOST)

	if pantahubHost == "" {
		pantahubHost = "localhost"
	}

	pantahubPort := utils.GetEnv(utils.ENV_PANTAHUB_PORT)
	pantahubScheme := utils.GetEnv(utils.ENV_PANTAHUB_SCHEME)

	pantahubHttpsUrl = pantahubScheme + "://" + pantahubHost

	if pantahubPort != "" {
		pantahubHttpsUrl += ":" + pantahubPort
	}
}

func PantahubS3Production() bool {
	return pantahubS3Production
}

func PantahubS3Path() string {
	return pantahubS3Path
}

func PantahubS3DevUrl() string {
	return pantahubHttpsUrl
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func MakeStorageId(owner string, sha string) string {
	res := sha256.Sum256([]byte(owner + "/" + sha))
	newSha := res[:]
	hexRes := make([]byte, hex.EncodedLen(len(newSha)))
	hex.Encode(hexRes, newSha)
	return string(hexRes)
}

func (a *ObjectsApp) handle_postobject(w rest.ResponseWriter, r *rest.Request) {

	newObject := Object{}

	r.DecodeJsonPayload(&newObject)

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}
	ownerStr, ok := owner.(string)
	if !ok {
		// XXX: find right error
		rest.Error(w, "Invalid Access Token", http.StatusForbidden)
		return
	}

	newObject.Owner = ownerStr

	collection := a.mgoSession.DB("").C("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	storageId := MakeStorageId(ownerStr, newObject.Sha)
	newObject.StorageId = storageId
	newObject.Id = newObject.Sha

	SyncObjectSizes(&newObject)

	result, err := CalcUsageAfterPost(ownerStr, a.mgoSession, bson.ObjectId(newObject.Id), newObject.SizeInt)

	if err != nil {
		rest.Error(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := GetDiskQuota(ownerStr)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		rest.Error(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {

		log.Println("Quota exceeded in post object.")
		rest.Error(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	err = collection.Insert(newObject)

	if err != nil {
		w.WriteHeader(http.StatusConflict)
		w.Header().Add("X-PH-Error", "Error inserting object into database "+err.Error())
		// we return anyway with the already available info about this object
	}

	issuerUrl := utils.GetApiEndpoint("/objects")
	newObjectWithAccess := MakeObjAccessible(issuerUrl, ownerStr, newObject, storageId)
	w.WriteJson(newObjectWithAccess)
}

func (a *ObjectsApp) handle_putobject(w rest.ResponseWriter, r *rest.Request) {

	newObject := Object{}

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)
	if !ok {
		// XXX: find right error
		rest.Error(w, "Invalid Access", http.StatusForbidden)
		return
	}
	putId := r.PathParam("id")
	storageId := MakeStorageId(ownerStr, putId)

	err := collection.FindId(storageId).One(&newObject)

	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner != owner {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newObject)

	newObject.Owner = owner.(string)
	newObject.StorageId = storageId
	newObject.Id = putId

	SyncObjectSizes(&newObject)

	result, err := CalcUsageAfterPut(ownerStr, a.mgoSession, bson.ObjectId(putId), newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		rest.Error(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := GetDiskQuota(ownerStr)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		rest.Error(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		rest.Error(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	collection.UpsertId(storageId, newObject)

	w.WriteJson(newObject)
}

func GetDiskQuota(prn string) (float64, error) {

	quota := "2GiB"

	// hack until account plans are properly implemented
	if prn == "prn:pantahub.com:auth:/user1" {
		quota = "25GiB"
	}

	uM, err := units.ParseStrictBytes(quota)
	if err != nil {
		return 0, err
	}

	return float64(uM), err
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

	if PantahubS3Production() {
		svc := s3.New(session.New(&aws.Config{Region: aws.String("us-east-1")}))

		// GET URL
		req, _ := svc.GetObjectRequest(&s3.GetObjectInput{
			Bucket: aws.String("systemcloud-001"),
			Key:    aws.String(storageId),
		})
		urlStr, _ := req.Presign(15 * time.Minute)
		filesObjWithAccess.SignedGetUrl = urlStr

		if Subject == obj.Owner {
			// PUT URL
			req, _ = svc.PutObjectRequest(&s3.PutObjectInput{
				Bucket: aws.String("systemcloud-001"),
				Key:    aws.String(storageId),
			})
			urlStr, _ = req.Presign(15 * time.Minute)
			filesObjWithAccess.SignedPutUrl = urlStr
		}

		filesObjWithAccess.Now = strconv.FormatInt(req.Time.Unix(), 10)
		filesObjWithAccess.ExpireTime = strconv.FormatInt(int64(req.ExpireTime.Seconds()), 10)
	} else {
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
		objAccessTokGet := NewObjectAccessForSec(obj.ObjectName, http.MethodGet, size, Issuer, Subject, storageId, 60)
		tokGet, err := objAccessTokGet.Sign()
		if err != nil {
			log.Println("INTERNAL ERROR local-s3: " + err.Error())
			filesObjWithAccess.SignedGetUrl = PantahubS3DevUrl() + "/local-s3/INTERNAL-ERROR"
		} else {
			filesObjWithAccess.SignedGetUrl = PantahubS3DevUrl() + "/local-s3/" + tokGet
		}

		if Subject == obj.Owner {
			objAccessTokPut := NewObjectAccessForSec(obj.ObjectName, http.MethodPut, size, Issuer, Subject, storageId, 60)
			tokPut, err := objAccessTokPut.Sign()
			if err != nil {
				log.Println("INTERNAL ERROR local-s3: " + err.Error())
				filesObjWithAccess.SignedPutUrl = PantahubS3DevUrl() + "/local-s3/INTERNAL-ERROR"
			} else {
				filesObjWithAccess.SignedPutUrl = PantahubS3DevUrl() + "/local-s3/" + tokPut
			}
		}
	}
	return filesObjWithAccess
}

func (a *ObjectsApp) handle_getobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["owner"]
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
		// XXX: find right error
		if !ok {
			rest.Error(w, "You need to be logged in as USER or DEVICE with owner", http.StatusForbidden)
			return
		}
	}

	collection := a.mgoSession.DB("").C("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		rest.Error(w, "Invalid Access", http.StatusForbidden)
		return
	}

	objId := r.PathParam("id")
	storageId := MakeStorageId(ownerStr, objId)

	var filesObj Object
	err := collection.FindId(storageId).One(&filesObj)

	if err != nil {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if filesObj.Owner != owner {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	issuerUrl := utils.GetApiEndpoint("/objects")
	filesObjWithAccess := MakeObjAccessible(issuerUrl, ownerStr, filesObj, storageId)

	w.WriteJson(filesObjWithAccess)
}

func (a *ObjectsApp) handle_getobjectfile(w rest.ResponseWriter, r *rest.Request) {

	// XXX: refactor: dupe code with getobject with getobject
	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["owner"]
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
		// XXX: find right error
		if !ok {
			rest.Error(w, "You need to be logged in as USER or DEVICE with owner", http.StatusForbidden)
			return
		}
	}

	collection := a.mgoSession.DB("").C("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		rest.Error(w, "Invalid Access", http.StatusForbidden)
		return
	}

	objId := r.PathParam("id")
	storageId := MakeStorageId(ownerStr, objId)

	var filesObj Object
	err := collection.FindId(storageId).One(&filesObj)

	if err != nil {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if filesObj.Owner != owner {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	issuerUrl := utils.GetApiEndpoint("/objects")
	filesObjWithAccess := MakeObjAccessible(issuerUrl, ownerStr, filesObj, storageId)

	url := filesObjWithAccess.SignedGetUrl

	w.Header().Add("Location", url)
	w.WriteHeader(http.StatusFound)
}

func (a *ObjectsApp) handle_getobjects(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	filter := r.URL.Query().Get("filter")
	m := map[string]interface{}{}

	if filter != "" {
		err := json.Unmarshal([]byte(filter), &m)
		if err != nil {
			rest.Error(w, "Error parsing filter json "+err.Error(), http.StatusInternalServerError)
		}
	}
	m["owner"] = owner

	newObjects := make([]Object, 0)
	collection.Find(m).All(&newObjects)

	w.WriteJson(newObjects)
}

func (a *ObjectsApp) handle_deleteobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ownerStr, ok := owner.(string)

	if !ok {
		// XXX: find right error
		rest.Error(w, "Invalid Access", http.StatusForbidden)
		return
	}

	delId := r.PathParam("id")
	storageId := MakeStorageId(ownerStr, delId)

	newObject := Object{}

	collection.FindId(storageId).One(&newObject)

	if newObject.Owner == owner {
		collection.RemoveId(storageId)
	}

	w.WriteJson(newObject)
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *ObjectsApp {

	app := new(ObjectsApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	// XXX: allow config through env
	app.awsS3Bucket = "systemcloud-001"
	app.awsRegion = "us-east-1"

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{Logger: log.New(os.Stdout,
		"/objects:", log.Lshortfile)})
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

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwt_middleware,
	})

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", handle_auth),
		rest.Get("/", app.handle_getobjects),
		rest.Post("/", app.handle_postobject),
		rest.Get("/:id", app.handle_getobject),
		rest.Get("/:id/blob", app.handle_getobjectfile),
		rest.Put("/:id", app.handle_putobject),
		rest.Delete("/:id", app.handle_deleteobject),
	)
	app.Api.SetApp(api_router)

	return app
}
