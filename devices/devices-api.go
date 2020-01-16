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
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	petname "github.com/dustinkirkland/golang-petname"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/gcapi"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/resty.v1"
)

// PantahubDevicesAutoTokenV1 device auto token name
const PantahubDevicesAutoTokenV1 = "Pantahub-Devices-Auto-Token-V1"

//DeviceNickRule : Device nick rule used to create/update a device nick
const DeviceNickRule = `(?m)^[a-zA-Z0-9_\-+%]+$`

// App Web app structure
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// ModelError error type
type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Device device structure
type Device struct {
	ID           primitive.ObjectID     `json:"id" bson:"_id"`
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

func init() {
	// seed this for petname as dustin dropped our patch upstream... moo
	rand.Seed(time.Now().Unix())
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func (a *App) handlePatchUserData(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "USER" {
		utils.RestErrorWrapper(w, "User data can only be updated by User", http.StatusBadRequest)
		return
	}
	deviceID, err := a.ParseDeviceIDOrNick(r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}
	data := map[string]interface{}{}
	err = r.DecodeJsonPayload(&data)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var device Device
	err = collection.FindOne(ctx,
		bson.M{
			"_id":     deviceID,
			"garbage": bson.M{"$ne": true},
		}).
		Decode(&device)

	for k, v := range data {
		device.UserMeta[k] = v
	}

	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   deviceID,
			"owner": owner.(string),
		},
		bson.M{"$set": bson.M{
			"user-meta":    device.UserMeta,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device user-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}

func (a *App) handlePutUserData(w rest.ResponseWriter, r *rest.Request) {
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "USER" {
		utils.RestErrorWrapper(w, "User data can only be updated by User", http.StatusBadRequest)
		return
	}

	deviceID := r.PathParam("id")

	data := map[string]interface{}{}
	err := r.DecodeJsonPayload(&data)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   deviceObjectID,
			"owner": owner.(string),
		},
		bson.M{"$set": bson.M{
			"user-meta":    data,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device user-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}

func (a *App) handlePutDeviceData(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Device data can only be updated by Device", http.StatusBadRequest)
		return
	}

	deviceID := r.PathParam("id")
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{}
	err = r.DecodeJsonPayload(&data)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	data = utils.BsonQuoteMap(&data)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id": deviceObjectID,
			"prn": owner.(string),
		},
		bson.M{"$set": bson.M{
			"device-meta":  data,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device user-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device user-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&data))
}

func (a *App) handlePostDevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	r.DecodeJsonPayload(&newDevice)

	mgoid := bson.NewObjectId()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newDevice.ID = ObjectID
	newDevice.Prn = "prn:::devices:/" + newDevice.ID.Hex()

	// if user does not provide a secret, we invent one ...
	if newDevice.Secret == "" {
		var err error
		newDevice.Secret, err = utils.GenerateSecret(15)
		if err != nil {
			utils.RestErrorWrapper(w, "Error generating secret", http.StatusInternalServerError)
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
				utils.RestErrorWrapper(w, "Error using AutoAuthToken "+err.Error(), http.StatusBadRequest)
				return
			}

			// update owner and usermeta
			newDevice.Owner = autoInfo.Owner
			if autoInfo.UserMeta != nil {
				newDevice.UserMeta = autoInfo.UserMeta
			}
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

	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isValidNick {
		utils.RestErrorWrapper(w, "Invalid Device Nick (Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": ObjectID},
		bson.M{"$set": newDevice},
		updateOptions,
	)

	if err != nil {
		log.Print(newDevice)
		utils.RestErrorWrapper(w, "Error creating device "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(newDevice)
}

func (a *App) handlePutDevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	putID := r.PathParam("id")

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx,
		bson.M{"_id": deviceObjectID}).
		Decode(&newDevice)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
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

	if callerIsDevice && newDevice.Prn != authID {
		utils.RestErrorWrapper(w, "Not Device Accessible Resource Id", http.StatusForbidden)
		return
	}

	if callerIsUser && newDevice.Owner != "" && newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newDevice)

	if newDevice.ID.Hex() != putID {
		utils.RestErrorWrapper(w, "Cannot change device Id in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Prn != prn {
		utils.RestErrorWrapper(w, "Cannot change device prn in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Owner != owner {
		utils.RestErrorWrapper(w, "Cannot change device owner in PUT", http.StatusForbidden)
		return
	}

	if newDevice.TimeCreated != timeCreated {
		utils.RestErrorWrapper(w, "Cannot change device timeCreated in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Secret == "" {
		utils.RestErrorWrapper(w, "Empty Secret not allowed for devices in PUT", http.StatusForbidden)
		return
	}

	if callerIsDevice && newDevice.IsPublic != isPublic {
		utils.RestErrorWrapper(w, "Device cannot change its own 'public' state", http.StatusForbidden)
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
			newDevice.Owner = authID.(string)
			newDevice.Challenge = ""
		} else {
			utils.RestErrorWrapper(w, "No Access to Device", http.StatusForbidden)
			return
		}
	}

	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isValidNick {
		utils.RestErrorWrapper(w, "Invalid Device Nick(Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
		return
	}

	newDevice.TimeModified = time.Now()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": newDevice},
		updateOptions,
	)

	// unquote back to original format
	newDevice.UserMeta = utils.BsonUnquoteMap(&newDevice.UserMeta)
	newDevice.DeviceMeta = utils.BsonUnquoteMap(&newDevice.DeviceMeta)

	w.WriteJson(newDevice)
}

//ParseDeviceIDOrNick : Parse DeviceID Or Nick from the given string and return device objectID
func (a *App) ParseDeviceIDOrNick(param string) (*primitive.ObjectID, error) {
	mgoid, err := primitive.ObjectIDFromHex(param)
	if err != nil {
		return a.LookupDeviceNick(param)
	}
	return &mgoid, nil
}

func (a *App) handleDetDevice(w rest.ResponseWriter, r *rest.Request) {
	var device Device
	mgoid, err := a.ParseDeviceIDOrNick(r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collectionAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collectionAccounts == nil {
		utils.RestErrorWrapper(w, "Error with Database (accounts) connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx,
		bson.M{
			"_id":     mgoid,
			"garbage": bson.M{"$ne": true},
		}).
		Decode(&device)

	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	if !device.IsPublic {
		// XXX: fixme; needs delegation of authorization for device accessing its resources
		// could be subscriptions, but also something else
		if callerIsDevice && device.Prn != authID {
			utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
			return
		}

		if callerIsUser && device.Owner != authID {
			utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
			return
		}
	} else if authID != device.Prn && authID != device.Owner {
		device.Secret = ""
		device.Challenge = ""
		device.UserMeta = map[string]interface{}{}
		device.DeviceMeta = map[string]interface{}{}
	}

	if device.Owner != "" {
		var ownerAccount accounts.Account

		// first check default accounts like user1, user2, etc...
		ownerAccount, ok := accounts.DefaultAccounts[device.Owner]
		if !ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := collectionAccounts.FindOne(ctx,
				bson.M{"prn": device.Owner}).
				Decode(&ownerAccount)

			if err != nil {
				utils.RestErrorWrapper(w, "Owner account not Found", http.StatusInternalServerError)
				return
			}
		}
		device.OwnerNick = ownerAccount.Nick
	}

	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	w.WriteJson(device)
}

func (a *App) handleGetUserDevice(w rest.ResponseWriter, r *rest.Request) {

	var device Device
	var account accounts.Account

	usernick := r.PathParam("usernick")
	devicenick := r.PathParam("devicenick")

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else if authType == "USER" {
		callerIsUser = true
	} else {
		utils.RestErrorWrapper(w, "You need to be logged in with either USER or DEVICE account type.", http.StatusForbidden)
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

		collAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
		if collAccounts == nil {
			utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := collAccounts.FindOne(ctx,
			bson.M{"nick": usernick}).
			Decode(&account)

		if err != nil {
			log.Println("ERROR: error getting account by nick; will return Forbidden to cover up details from backend: " + err.Error())
			utils.RestErrorWrapper(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	collDevices := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collDevices == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := collDevices.FindOne(ctx, bson.M{
		"nick":    devicenick,
		"owner":   account.Prn,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)

	if err != nil {
		log.Println("ERROR: error getting device by nick: " + err.Error())
		utils.RestErrorWrapper(w, "Forbidden", http.StatusForbidden)
		return
	}

	if !device.IsPublic {
		// XXX: fixme; needs delegation of authorization for device accessing its resources
		// could be subscriptions, but also something else
		if callerIsDevice && device.Prn != authID {
			utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
			return
		}

		if callerIsUser && device.Owner != authID {
			utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
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

func (a *App) handlePatchDevice(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	patchID := r.PathParam("id")

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" {
		utils.RestErrorWrapper(w, "Devices cannot change their own public state.", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceID, err := primitive.ObjectIDFromHex(patchID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner == "" || newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	patch := Device{}
	patched := false

	err = r.DecodeJsonPayload(&patch)

	if err != nil {
		utils.RestErrorWrapper(w, "Internal Error (decode patch)", http.StatusInternalServerError)
		return
	}
	if patch.Nick != "" {
		newDevice.Nick = patch.Nick
		patched = true
	}
	isValidNick, err := regexp.MatchString(DeviceNickRule, newDevice.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Validating Device nick "+err.Error(), http.StatusBadRequest)
		return
	}
	if !isValidNick {
		utils.RestErrorWrapper(w, "Invalid Device Nick(Only allowed characters:[A-Za-z0-9-_+%])", http.StatusBadRequest)
		return
	}

	if patched {
		newDevice.TimeModified = time.Now()
		updateOptions := options.Update()
		updateOptions.SetUpsert(true)
		_, err = collection.UpdateOne(
			ctx,
			bson.M{"_id": newDevice.ID},
			bson.M{"$set": newDevice},
			updateOptions,
		)
		if err != nil {
			utils.RestErrorWrapper(w, "Error updating patched device state", http.StatusForbidden)
			return
		}
	}

	newDevice.Challenge = ""
	newDevice.Secret = ""

	w.WriteJson(newDevice)
}

func (a *App) handlePatchDeviceData(w rest.ResponseWriter, r *rest.Request) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	device := Device{}

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var owner interface{}
	owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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

	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Device data can only be updated by Device", http.StatusBadRequest)
		return
	}

	deviceID, err := a.ParseDeviceIDOrNick(r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	data := map[string]interface{}{}
	err = r.DecodeJsonPayload(&data)
	if err != nil {
		utils.RestErrorWrapper(w, "Error parsing data: "+err.Error(), http.StatusBadRequest)
		return
	}
	for k, v := range data {
		device.DeviceMeta[k] = v
		if v == nil {
			delete(device.DeviceMeta, k)
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id": deviceID,
			"prn": owner.(string),
		},
		bson.M{"$set": bson.M{
			"device-meta":  device.DeviceMeta,
			"timemodified": time.Now(),
		}},
	)
	if updateResult.MatchedCount == 0 {
		utils.RestErrorWrapper(w, "Error updating device-meta: not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device-meta: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&device.DeviceMeta))
}

func (a *App) handlePutPublic(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	putID := r.PathParam("id")

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" {
		utils.RestErrorWrapper(w, "Devices cannot change their own public state.", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner != "" && newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	newDevice.IsPublic = true
	newDevice.TimeModified = time.Now()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": newDevice},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newDevice)
}

func (a *App) handleDeletePublic(w rest.ResponseWriter, r *rest.Request) {
	newDevice := Device{}
	putID := r.PathParam("id")

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" {
		utils.RestErrorWrapper(w, "Devices cannot change their own public state.", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deviceObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newDevice)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newDevice.Owner != "" && newDevice.Owner != authID {
		utils.RestErrorWrapper(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	newDevice.IsPublic = false
	newDevice.TimeModified = time.Now()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newDevice.ID},
		bson.M{"$set": newDevice},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error updating device public state", http.StatusForbidden)
		return
	}

	w.WriteJson(newDevice)
}

func (a *App) handleGetDevices(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	devices := make([]Device, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}

	for k, v := range r.URL.Query() {
		if query[k] == nil {
			if strings.HasPrefix(v[0], "!") {
				v[0] = strings.TrimPrefix(v[0], "!")
				query[k] = bson.M{"$ne": v[0]}
			} else if strings.HasPrefix(v[0], "^") {
				v[0] = strings.TrimPrefix(v[0], "^")
				query[k] = bson.M{"$regex": "^" + v[0], "$options": "i"}
			} else {
				query[k] = v[0]
			}
		}
	}

	cur, err := collection.Find(ctx, query, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching devices:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Device{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		result.UserMeta = utils.BsonUnquoteMap(&result.UserMeta)
		result.DeviceMeta = utils.BsonUnquoteMap(&result.DeviceMeta)
		devices = append(devices, result)
	}

	w.WriteJson(devices)
}

func (a *App) handleDeleteDevice(w rest.ResponseWriter, r *rest.Request) {
	delID := r.PathParam("id")

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	device := Device{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(delID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Println("Error deleting device: " + err.Error())
			utils.RestErrorWrapper(w, "Device not found", http.StatusInternalServerError)
			return
		}

		device.ID = deviceObjectID
		w.WriteJson(device)
		return
	}

	if device.Owner == owner {
		result, res := MarkDeviceAsGarbage(w, delID)
		if res.StatusCode() != 200 {
			log.Print(res)
			log.Print(result)
			utils.RestErrorWrapper(w, "Error calling GC API for Marking Device Garbage", http.StatusInternalServerError)
			return
		}
		if result.Status == 1 {
			device.Garbage = true
		}
	}

	w.WriteJson(device)
}

// MarkDeviceAsGarbage : Mark Device as Garbage
func MarkDeviceAsGarbage(
	w rest.ResponseWriter,
	deviceID string,
) (
	gcapi.MarkDeviceGarbage,
	*resty.Response,
) {
	response := gcapi.MarkDeviceGarbage{}
	APIEndPoint := utils.GetEnv("PANTAHUB_GC_API") + "/markgarbage/device/" + deviceID
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		utils.RestErrorWrapper(w, "internal error calling test server: "+err.Error(), http.StatusInternalServerError)
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res
}

// LookupDeviceNick : Lookup Device Nicks and return device id
func (a *App) LookupDeviceNick(deviceID string) (*primitive.ObjectID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}
	count, err := collection.CountDocuments(ctx,
		bson.M{
			"nick":    deviceID,
			"garbage": bson.M{"$ne": true},
		})
	if err != nil {
		return nil, errors.New("Error finding device:" + deviceID + ",err:" + err.Error())
	}
	if count > 0 {
		deviceObject := Device{}
		err = collection.FindOne(ctx,
			bson.M{
				"nick":    deviceID,
				"garbage": bson.M{"$ne": true},
			}).
			Decode(&deviceObject)
		if err != nil {
			return nil, errors.New("Error finding device:" + deviceID + ",err:" + err.Error())
		}
		return &deviceObject.ID, nil

	}
	return nil, errors.New("Device not found")
}

// New create devices web app
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "timemodified", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}
	// Indexing for the owner,garbage fields
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}
	// Indexing for the device,garbage fields
	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	err = app.EnsureTokenIndices()
	if err != nil {
		log.Println("Error creating indices for pantahub devices tokens: " + err.Error())
		return nil
	}

	app.API = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/devices:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "devices"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})

	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
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

	app.API.Use(&rest.IfMiddleware{
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
		IfTrue: app.jwtMiddleware,
	})
	app.API.Use(&rest.IfMiddleware{
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
		IfTrue: &utils.AuthMiddleware{},
	})

	writeDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.WriteDevices,
	}
	readDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	}
	updateDevicesScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.UpdateDevices,
	}

	// /auth_status endpoints
	apiRouter, _ := rest.MakeRouter(
		// token api
		rest.Post("/tokens", utils.ScopeFilter(readDevicesScopes, app.handlePostTokens)),
		rest.Delete("/tokens/:id", utils.ScopeFilter(updateDevicesScopes, app.handleDisableTokens)),
		rest.Get("/tokens", utils.ScopeFilter(readDevicesScopes, app.handleGetTokens)),

		// default api
		rest.Get("/auth_status", utils.ScopeFilter(readDevicesScopes, handleAuth)),
		rest.Get("/", utils.ScopeFilter(readDevicesScopes, app.handleGetDevices)),
		rest.Post("/", utils.ScopeFilter(writeDevicesScopes, app.handlePostDevice)),
		rest.Get("/:id", utils.ScopeFilter(readDevicesScopes, app.handleDetDevice)),
		rest.Put("/:id", utils.ScopeFilter(writeDevicesScopes, app.handlePutDevice)),
		rest.Patch("/:id", utils.ScopeFilter(writeDevicesScopes, app.handlePatchDevice)),
		rest.Put("/:id/public", utils.ScopeFilter(writeDevicesScopes, app.handlePutPublic)),
		rest.Delete("/:id/public", utils.ScopeFilter(writeDevicesScopes, app.handleDeletePublic)),
		rest.Put("/:id/user-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePutUserData)),
		rest.Patch("/:id/user-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePatchUserData)),
		rest.Put("/:id/device-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePutDeviceData)),
		rest.Patch("/:id/device-meta", utils.ScopeFilter(writeDevicesScopes, app.handlePatchDeviceData)),
		rest.Delete("/:id", utils.ScopeFilter(writeDevicesScopes, app.handleDeleteDevice)),
		// lookup by nick-path (np)
		rest.Get("/np/:usernick/:devicenick", utils.ScopeFilter(readDevicesScopes, app.handleGetUserDevice)),
	)
	app.API.SetApp(apiRouter)

	return app
}
