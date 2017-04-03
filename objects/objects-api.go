//
// Copyright 2016,2017  Alexander Sack <asac129@gmail.com>
//
package objects

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"pantahub-base/utils"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"gopkg.in/mgo.v2"
)

type ObjectsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
	awsS3Bucket    string
	awsRegion      string
}

type Object struct {
	Id         string `json:"id" bson:"id"`
	StorageId  string `json:"storage-id" bson:"_id"`
	Owner      string `json:"owner"`
	ObjectName string `json:"objectname"`
	Sha        string `json:"sha256sum"`
	Size       string `json:"size"`
	MimeType   string `json:"mime-type"`
}

type ObjectWithAccess struct {
	Object       `bson:",inline"`
	SignedPutUrl string `json:"signed-puturl"`
	SignedGetUrl string `json:"signed-geturl"`
	Now          string `json:"now"`
	ExpireTime   string `json:"expire-time"`
}

var pantahubS3Path string
var pantahubS3Production bool
var pantahubHttpsUrl string

func init() {
	pantahubS3Path = os.Getenv("PANTAHUB_S3PATH")

	if pantahubS3Path == "production" {
		pantahubS3Production = true
	} else {
		pantahubS3Production = false
	}

	if pantahubS3Path == "" {
		pantahubS3Path = "./local-s3/"
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
	fmt.Println("storeid: " + storageId)

	err := collection.Insert(newObject)

	if err != nil {
		w.WriteHeader(http.StatusConflict)
		w.Header().Add("X-PH-Error", "Error inserting object into database "+err.Error())
	}

	newObjectWithAccess := a.makeObjAccessible(newObject, storageId)
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

	collection.UpsertId(storageId, newObject)

	w.WriteJson(newObject)
}

func (a *ObjectsApp) makeObjAccessible(obj Object, storageId string) ObjectWithAccess {
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

		// PUT URL
		req, _ = svc.PutObjectRequest(&s3.PutObjectInput{
			Bucket: aws.String("systemcloud-001"),
			Key:    aws.String(storageId),
		})
		urlStr, _ = req.Presign(15 * time.Minute)
		filesObjWithAccess.SignedPutUrl = urlStr

		filesObjWithAccess.Now = strconv.FormatInt(req.Time.Unix(), 10)
		filesObjWithAccess.ExpireTime = strconv.FormatInt(int64(req.ExpireTime.Seconds()), 10)
	} else {
		filesObjWithAccess.SignedGetUrl = PantahubS3DevUrl() + "/local-s3/" + storageId
		filesObjWithAccess.SignedPutUrl = PantahubS3DevUrl() + "/local-s3/" + storageId

		timeNow := time.Now()
		filesObjWithAccess.Now = strconv.FormatInt(timeNow.Unix(), 10)
		duration, _ := time.ParseDuration("15m")
		filesObjWithAccess.ExpireTime = strconv.FormatInt(timeNow.Add(duration).Unix(), 10)
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
	filesObjWithAccess := a.makeObjAccessible(filesObj, storageId)

	w.WriteJson(filesObjWithAccess)
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
	app.Api.Use(&rest.AccessLogApacheMiddleware{})
	app.Api.Use(rest.DefaultCommonStack...)

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
		rest.Put("/:id", app.handle_putobject),
		rest.Delete("/:id", app.handle_deleteobject),
	)
	app.Api.SetApp(api_router)

	return app
}
