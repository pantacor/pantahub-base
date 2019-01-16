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
package devices

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	petname "github.com/dustinkirkland/golang-petname"
	jwt "github.com/fundapps/go-json-rest-middleware-jwt"
	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/gcapi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const PantahubDevicesAutoTokenV1 = "Pantahub-Devices-Auto-Token-V1"

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
	OwnerNick    string                 `json:"owner-nick,omitempty" bson:"-"`
	Secret       string                 `json:"secret,omitempty"`
	TimeCreated  time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified time.Time              `json:"time-modified" bson:"timemodified"`
	Challenge    string                 `json:"challenge,omitempty"`
	IsPublic     bool                   `json:"public"`
	UserMeta     map[string]interface{} `json:"user-meta" bson:"user-meta"`
	DeviceMeta   map[string]interface{} `json:"device-meta" bson:"device-meta"`
	Garbage      bool                   `json:"garbage" bson:"garbage"`
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
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
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

	err = collection.Update(bson.M{"_id": bsonId, "owner": owner.(string)},
		bson.M{"$set": bson.M{"user-meta": data, "timemodified": time.Now()}})
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
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
		return
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
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

	err = collection.Update(bson.M{"_id": bsonId, "prn": owner.(string)}, bson.M{"$set": bson.M{"device-meta": data, "timemodified": time.Now()}})
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

	// if user does not provide a secret, we invent one ...
	if newDevice.Secret == "" {
		var err error
		newDevice.Secret, err = utils.GenerateSecret(15)
		if err != nil {
			rest.Error(w, "Error generating secret", http.StatusInternalServerError)
			return
		}
	}

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]

	var owner interface{}
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

		// lets see if we have an auto assign candidate
		autoAuthToken := r.Header.Get(PantahubDevicesAutoTokenV1)

		if autoAuthToken != "" {

			autoInfo, err := a.getBase64AutoTokenInfo(autoAuthToken)
			if err != nil {
				rest.Error(w, "Error using AutoAuthToken "+err.Error(), http.StatusBadRequest)
				return
			}

			// update owner and usermeta
			newDevice.Owner = autoInfo.Owner
			newDevice.UserMeta = autoInfo.UserMeta
		} else {
			newDevice.Challenge = petname.Generate(3, "-")
		}
	}

	newDevice.TimeCreated = time.Now()
	newDevice.TimeModified = newDevice.TimeCreated

	// we invent a nick for user in case he didnt ask for a specfic one...
	if newDevice.Nick == "" {
		newDevice.Nick = petname.Generate(3, "_")
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

	authId, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

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

	authId, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

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

	collectionAccounts := a.mgoSession.DB("").C("pantahub_accounts")

	if collectionAccounts == nil {
		rest.Error(w, "Error with Database (accounts) connectivity", http.StatusInternalServerError)
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

	if device.Owner != "" {
		var ownerAccount accounts.Account

		// first check default accounts like user1, user2, etc...
		ownerAccount, ok := accounts.DefaultAccounts[device.Owner]
		if !ok {
			err = collectionAccounts.Find(bson.M{"prn": device.Owner}).One(&ownerAccount)

			if err != nil {
				rest.Error(w, "Owner account not Found", http.StatusInternalServerError)
				return
			}
		}
		device.OwnerNick = ownerAccount.Nick
	}

	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	w.WriteJson(device)
}

func (a *DevicesApp) handle_getuserdevice(w rest.ResponseWriter, r *rest.Request) {

	var device Device
	var account accounts.Account

	usernick := r.PathParam("usernick")
	devicenick := r.PathParam("devicenick")

	authId, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else if authType == "USER" {
		callerIsUser = true
	} else {
		rest.Error(w, "You need to be logged in with either USER or DEVICE account type.", http.StatusForbidden)
		return
	}

	// first check if we refer to a default accoutn
	isDefaultAccount := false
	for _, v := range accounts.DefaultAccounts {
		if v.Nick == usernick {
			account = v
			isDefaultAccount = true
			break
		}
	}

	// if not a default, lets look for proper accounts in db...
	if !isDefaultAccount {

		collAccounts := a.mgoSession.DB("").C("pantahub_accounts")
		if collAccounts == nil {
			rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		err := collAccounts.Find(bson.M{"nick": usernick}).One(&account)

		if err == mgo.ErrNotFound {
			log.Println("ERROR: error getting account by nick: " + err.Error())
			rest.Error(w, "Not Found", http.StatusNotFound)
			return
		} else if err != nil {
			log.Println("ERROR: error getting account by nick: " + err.Error())
			rest.Error(w, "Internal Error", http.StatusInternalServerError)
			return
		}
	}

	collDevices := a.mgoSession.DB("").C("pantahub_devices")

	if collDevices == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collDevices.Find(bson.M{"nick": devicenick, "owner": account.Prn}).One(&device)

	if err == mgo.ErrNotFound {
		log.Println("ERROR: error getting device by nick: " + err.Error())
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	} else if err != nil {
		log.Println("ERROR: error getting device by nick: " + err.Error())
		rest.Error(w, "Internal Error", http.StatusNotFound)
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
		device.Challenge = ""
	}

	// we always hide the secret
	device.Secret = ""
	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	w.WriteJson(device)
}

func (a *DevicesApp) handle_patchdevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	patchId := r.PathParam("id")

	authId, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

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

	err := collection.FindId(bson.ObjectIdHex(patchId)).One(&newDevice)
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner == "" || newDevice.Owner != authId {
		rest.Error(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	patch := Device{}
	patched := false

	err = r.DecodeJsonPayload(&patch)

	if err != nil {
		rest.Error(w, "Internal Error (decode patch)", http.StatusInternalServerError)
		return
	}
	if patch.Nick != "" {
		newDevice.Nick = patch.Nick
		patched = true
	}

	if patched {
		newDevice.TimeModified = time.Now()
		_, err = collection.UpsertId(newDevice.Id, newDevice)
		if mgo.IsDup(err) {
			rest.Error(w, "Device unique constraint violated", http.StatusConflict)
			return
		} else if err != nil {
			rest.Error(w, "Error updating patched device state", http.StatusForbidden)
			return
		}
	}

	newDevice.Challenge = ""
	newDevice.Secret = ""

	w.WriteJson(newDevice)
}

func (a *DevicesApp) handle_putpublic(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	putId := r.PathParam("id")

	authId, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

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

	authId, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

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
	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
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

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
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
		result := MarkDeviceAsGarbage(w, delId)
		if result.Status == 1 {
			device.Garbage = true
		}
	}

	w.WriteJson(device)
}

// MarkDeviceAsGarbage : Mark Device as Garbage
func MarkDeviceAsGarbage(w rest.ResponseWriter, deviceID string) gcapi.MarkDeviceGarbage {
	response := gcapi.MarkDeviceGarbage{}
	APIEndPoint := utils.GetEnv("PANTAHUB_GC_API") + "/markgarbage/device/" + deviceID
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		rest.Error(w, "internal error calling test server: "+err.Error(), http.StatusInternalServerError)
	}
	err = json.Unmarshal(res.Body(), &response)
	return response
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

	index = mgo.Index{
		Key:        []string{"timemodified"},
		Unique:     false,
		Background: true,
		Sparse:     false,
	}

	err = app.mgoSession.DB("").C("pantahub_devices").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"prn"},
		Unique:     false,
		Background: true,
		Sparse:     false,
	}

	err = app.mgoSession.DB("").C("pantahub_devices").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index prn for pantahub_devices: " + err.Error())
		return nil
	}

	err = app.EnsureTokenIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub devices tokens: " + err.Error())
		return nil
	}

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/devices:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "devices"})

	app.Api.Use(rest.DefaultCommonStack...)
	app.Api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
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
		// token api
		rest.Post("/tokens", app.handle_posttokens),
		rest.Delete("/tokens/:id", app.handle_disabletokens),
		rest.Get("/tokens", app.handle_gettokens),

		// default api
		rest.Get("/auth_status", handle_auth),
		rest.Get("/", app.handle_getdevices),
		rest.Post("/", app.handle_postdevice),
		rest.Get("/:id", app.handle_getdevice),
		rest.Put("/:id", app.handle_putdevice),
		rest.Patch("/:id", app.handle_patchdevice),
		rest.Put("/:id/public", app.handle_putpublic),
		rest.Delete("/:id/public", app.handle_deletepublic),
		rest.Put("/:id/user-meta", app.handle_putuserdata),
		rest.Put("/:id/device-meta", app.handle_putdevicedata),
		rest.Delete("/:id", app.handle_deletedevice),
		// lookup by nick-path (np)
		rest.Get("/np/:usernick/:devicenick", app.handle_getuserdevice),
	)
	app.Api.SetApp(api_router)

	return app
}
