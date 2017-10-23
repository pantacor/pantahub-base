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
package devices

import (
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	petname "github.com/dustinkirkland/golang-petname"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"gitlab.com/pantacor/pantahub-base/utils"
)

func init() {
	// seed this for petname as dustin dropped our patch upstream... moo
	rand.Seed(time.Now().Unix())
}

type DevicesApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
}

type Device struct {
	Id           bson.ObjectId          `json:"id" bson:"_id"`
	Prn          string                 `json:"prn"`
	Nick         string                 `json:"nick"`
	Owner        string                 `json:"owner"`
	Secret       string                 `json:"secret,omitempty"`
	TimeCreated  time.Time              `json:"time-created"`
	TimeModified time.Time              `json:"time-modified"`
	Challenge    string                 `json:"challenge,omitempty"`
	IsPublic     bool                   `json:"public"`
	UserMeta     map[string]interface{} `json:"user-meta" bson:"user-meta"`
	DeviceMeta   map[string]interface{} `json:"device-meta" bson:"device-meta"`
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func (a *DevicesApp) handle_putuserdata(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(map[string]interface{})["prn"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(map[string]interface{})["type"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
		return
	}

	if authType != "USER" {
		rest.Error(w, "User data can only be updated by User", http.StatusBadRequest)
		return
	}

	deviceId := r.PathParam("id")
	bsonId := bson.ObjectIdHex(deviceId)

	data := map[string]interface{}{}
	err := r.DecodeJsonPayload(&data)
	if err != nil {
		rest.Error(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mgoSession.DB("").C("pantahub_devices")
	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err = collection.Update(bson.M{"_id": bsonId, "owner": owner.(string)}, bson.M{"$set": bson.M{"user-meta": data}})
	if err != nil {
		rest.Error(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}

func (a *DevicesApp) handle_putdevicedata(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(map[string]interface{})["prn"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(map[string]interface{})["type"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
		return
	}

	if authType != "DEVICE" {
		rest.Error(w, "Device data can only be updated by Device", http.StatusBadRequest)
		return
	}

	deviceId := r.PathParam("id")
	bsonId := bson.ObjectIdHex(deviceId)

	data := map[string]interface{}{}
	err := r.DecodeJsonPayload(&data)
	if err != nil {
		rest.Error(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mgoSession.DB("").C("pantahub_devices")
	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err = collection.Update(bson.M{"_id": bsonId, "prn": owner.(string)}, bson.M{"$set": bson.M{"device-meta": data}})
	if err != nil {
		rest.Error(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}

func (a *DevicesApp) handle_postdevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	r.DecodeJsonPayload(&newDevice)

	mgoid := bson.NewObjectId()
	newDevice.Id = mgoid
	newDevice.Prn = "prn:::devices:/" + newDevice.Id.Hex()
	newDevice.Challenge = petname.Generate(3, "-")

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]

	var owner interface{}

	if ok {
		owner, ok = jwtPayload.(map[string]interface{})["prn"]
	}

	if ok {
		// user registering here...
		newDevice.Owner = owner.(string)
		newDevice.UserMeta = utils.BsonQuoteMap(&newDevice.UserMeta)
		newDevice.DeviceMeta = map[string]interface{}{}
	} else {
		// device speaking here...
		newDevice.Owner = ""
		newDevice.UserMeta = map[string]interface{}{}
		newDevice.DeviceMeta = utils.BsonQuoteMap(&newDevice.DeviceMeta)
	}

	newDevice.TimeCreated = time.Now()

	if newDevice.Nick == "" {
		newDevice.Nick = petname.Generate(2, "_")
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	_, err := collection.UpsertId(mgoid, newDevice)

	if err != nil {
		rest.Error(w, "Error creating device "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(newDevice)
}

func (a *DevicesApp) handle_putdevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	putId := r.PathParam("id")

	authId, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collection.FindId(bson.ObjectIdHex(putId)).One(&newDevice)

	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	prn := newDevice.Prn
	timeCreated := newDevice.TimeCreated
	owner := newDevice.Owner
	challenge := newDevice.Challenge
	challengeVal := r.FormValue("challenge")
	isPublic := newDevice.IsPublic
	userMeta := utils.BsonUnquoteMap(&newDevice.UserMeta)
	deviceMeta := utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	if callerIsDevice && newDevice.Prn != authId {
		rest.Error(w, "Not Device Accessible Resource Id", http.StatusForbidden)
		return
	}

	if callerIsUser && newDevice.Owner != "" && newDevice.Owner != authId {
		rest.Error(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newDevice)

	if newDevice.Id.Hex() != putId {
		rest.Error(w, "Cannot change device Id in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Prn != prn {
		rest.Error(w, "Cannot change device prn in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Owner != owner {
		rest.Error(w, "Cannot change device owner in PUT", http.StatusForbidden)
		return
	}

	if newDevice.TimeCreated != timeCreated {
		rest.Error(w, "Cannot change device timeCreated in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Secret == "" {
		rest.Error(w, "Empty Secret not allowed for devices in PUT", http.StatusForbidden)
		return
	}

	if callerIsDevice && newDevice.IsPublic != isPublic {
		rest.Error(w, "Device cannot change its own 'public' state", http.StatusForbidden)
		return
	}

	// if device puts info, always reset the user part of the data and vv.
	if callerIsDevice {
		newDevice.UserMeta = utils.BsonQuoteMap(&userMeta)
	} else {
		newDevice.DeviceMeta = utils.BsonQuoteMap(&deviceMeta)
	}

	/* in case someone claims the device like this, update owner */
	if len(challenge) > 0 {
		if challenge == challengeVal {
			newDevice.Owner = authId.(string)
			newDevice.Challenge = ""
		} else {
			rest.Error(w, "No Access to Device", http.StatusForbidden)
			return
		}
	}

	newDevice.TimeModified = time.Now()
	collection.UpsertId(newDevice.Id, newDevice)

	// unquote back to original format
	newDevice.UserMeta = utils.BsonUnquoteMap(&newDevice.UserMeta)
	newDevice.DeviceMeta = utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	w.WriteJson(newDevice)
}

func (a *DevicesApp) handle_getdevice(w rest.ResponseWriter, r *rest.Request) {

	var device Device

	mgoid := bson.ObjectIdHex(r.PathParam("id"))

	authId, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collection.FindId(mgoid).One(&device)

	if err != nil {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	if !device.IsPublic {
		// XXX: fixme; needs delegation of authorization for device accessing its resources
		// could be subscriptions, but also something else
		if callerIsDevice && device.Prn != authId {
			rest.Error(w, "No Access", http.StatusForbidden)
			return
		}

		if callerIsUser && device.Owner != authId {
			rest.Error(w, "No Access", http.StatusForbidden)
			return
		}
	} else if !callerIsDevice && !callerIsUser {
		device.Secret = ""
		device.Challenge = ""
	}
	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	w.WriteJson(device)
}

func (a *DevicesApp) handle_putpublic(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	putId := r.PathParam("id")

	authId, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" {
		rest.Error(w, "Devices cannot change their own public state.", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collection.FindId(bson.ObjectIdHex(putId)).One(&newDevice)
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner != "" && newDevice.Owner != authId {
		rest.Error(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	newDevice.IsPublic = true
	newDevice.TimeModified = time.Now()

	_, err = collection.UpsertId(newDevice.Id, newDevice)
	if err != nil {
		rest.Error(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newDevice)
}

func (a *DevicesApp) handle_deletepublic(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	putId := r.PathParam("id")

	authId, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" {
		rest.Error(w, "Devices cannot change their own public state.", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collection.FindId(bson.ObjectIdHex(putId)).One(&newDevice)
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner != "" && newDevice.Owner != authId {
		rest.Error(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	newDevice.IsPublic = false
	newDevice.TimeModified = time.Now()

	_, err = collection.UpsertId(newDevice.Id, newDevice)
	if err != nil {
		rest.Error(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newDevice)
}

type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (a *DevicesApp) handle_getdevices(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	devices := make([]Device, 0)

	collection.Find(bson.M{"owner": owner}).All(&devices)

	for k, v := range devices {
		v.UserMeta = utils.BsonUnquoteMap(&v.UserMeta)
		v.DeviceMeta = utils.BsonUnquoteMap(&v.DeviceMeta)
		devices[k] = v
	}

	w.WriteJson(devices)
}

func (a *DevicesApp) handle_deletedevice(w rest.ResponseWriter, r *rest.Request) {

	delId := r.PathParam("id")

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB("").C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	device := Device{}

	collection.FindId(bson.ObjectIdHex(delId)).One(&device)

	if device.Owner == owner {
		collection.RemoveId(bson.ObjectIdHex(delId))
	}

	w.WriteJson(device)
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *DevicesApp {

	app := new(DevicesApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	index := mgo.Index{
		Key:        []string{"nick"},
		Unique:     true,
		Background: true,
		Sparse:     false,
	}

	err := app.mgoSession.DB("").C("pantahub_devices").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{Logger: log.New(os.Stdout, "devices|", 0)})
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
			// if call is coming with authorization attempt, ensure JWT middleware
			// is used... otherwise let through anonymous POST for registration
			auth := request.Header.Get("Authorization")
			if auth != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(auth)), "bearer ") {
				return true
			}

			// post new device means to register... allow this unauthenticated
			return !(request.Method == "POST" && request.URL.Path == "/")
		},
		IfTrue: app.jwt_middleware,
	})

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", handle_auth),
		rest.Get("/", app.handle_getdevices),
		rest.Post("/", app.handle_postdevice),
		rest.Get("/:id", app.handle_getdevice),
		rest.Put("/:id", app.handle_putdevice),
		rest.Put("/:id/public", app.handle_putpublic),
		rest.Delete("/:id/public", app.handle_deletepublic),
		rest.Put("/:id/user-meta", app.handle_putuserdata),
		rest.Put("/:id/device-meta", app.handle_putdevicedata),
		rest.Delete("/:id", app.handle_deletedevice),
	)
	app.Api.SetApp(api_router)

	return app
}
