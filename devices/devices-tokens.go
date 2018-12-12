//
// Copyright 2018  Pantacor Ltd.
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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"

	petname "github.com/dustinkirkland/golang-petname"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type PantahubDevicesJoinToken struct {
	Id              bson.ObjectId          `json:"id" bson:"_id"`
	Prn             string                 `json:"prn"`
	Nick            string                 `json:"nick"`
	Owner           string                 `json:"owner"`
	Token           string                 `json:"token,omitempty"`
	TokenSha        []byte                 `json:"token-sha,omitempty"`
	DefaultUserMeta map[string]interface{} `json:"default-user-meta"`
	Disabled        bool                   `json:"disabled"`
	TimeCreated     time.Time              `json:"time-created"`
	TimeModified    time.Time              `json:"time-modified"`
}

func (a *DevicesApp) handle_posttokens(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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
		rest.Error(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return

	}

	req := PantahubDevicesJoinToken{}

	err := r.DecodeJsonPayload(&req)

	if err != nil && err != rest.ErrJsonPayloadEmpty {
		rest.Error(w, "error decoding request: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Id = bson.NewObjectId()
	req.Prn = utils.IdGetPrn(req.Id, "devices-tokens")

	if req.Nick == "" {
		req.Nick = petname.Generate(3, "_")
	}

	req.Owner = caller.(string)

	key := make([]byte, 24)

	_, err = rand.Read(key)
	if err != nil {
		rest.Error(w, "error generating random token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// calc sha for secret to store in DB
	shaSummer := sha256.New()
	_, err = shaSummer.Write(key)
	sum := make([]byte, shaSummer.Size())
	req.TokenSha = shaSummer.Sum(sum)

	// set timecreated/modified to NOW
	req.TimeCreated = time.Now()
	req.TimeModified = req.TimeCreated

	err = a.mgoSession.DB("").C("pantahub_devices_tokens").Insert(&req)

	if err != nil {
		rest.Error(w, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// do not return TokenSha, return encoded key
	req.Token = base64.StdEncoding.EncodeToString(key)
	req.TokenSha = nil

	w.WriteJson(&req)
}

func (a *DevicesApp) handle_disabletokens(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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
		rest.Error(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	r.ParseForm()
	tokenId := r.PathParam("id")
	tokenIdBson := bson.ObjectIdHex(tokenId)

	err := a.mgoSession.DB("").C("pantahub_devices_tokens").
		Update(
			bson.M{"_id": tokenIdBson, "owner": caller.(string)},
			bson.M{"$set": bson.M{"disabled": true}},
		)

	if err != nil {
		rest.Error(w, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(bson.M{"status": "OK"})
}

func (a *DevicesApp) handle_gettokens(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		rest.Error(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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
		rest.Error(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	res := []PantahubDevicesJoinToken{}
	err := a.mgoSession.DB("").C("pantahub_devices_tokens").Find(bson.M{"owner": caller.(string)}).All(&res)

	if err != nil {
		rest.Error(w, "error getting device tokens for user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// lets not reveal details about token when collection gets queried
	for i, v := range res {
		v.TokenSha = nil
		v.Token = ""
		res[i] = v
	}

	w.WriteJson(res)
}

type autoTokenInfo struct {
	Owner    string
	UserMeta map[string]interface{}
}

// helper function to make it easy to get info based on auth auth token...
func (a *DevicesApp) getBase64AutoTokenInfo(tokenBase64 string) (*autoTokenInfo, error) {

	tok := make([]byte, 24)

	_, err := base64.StdEncoding.Decode(tok, []byte(tokenBase64))
	if err != nil {
		return nil, err
	}

	shaSummer := sha256.New()
	_, err = shaSummer.Write(tok)

	sum := make([]byte, shaSummer.Size())
	tokenSha := shaSummer.Sum(sum)

	res := PantahubDevicesJoinToken{}

	err = a.mgoSession.DB("").C("pantahub_devices_tokens").Find(bson.M{"tokensha": tokenSha}).One(&res)
	if err != nil {
		return nil, errors.New("token not found")
	}

	if res.Disabled {
		return nil, errors.New("token disabled")
	}

	result := autoTokenInfo{}
	result.Owner = res.Owner
	result.UserMeta = res.DefaultUserMeta

	return &result, nil
}

func (a *DevicesApp) EnsureTokenIndices() error {

	index := mgo.Index{
		Key:        []string{"owner"},
		Unique:     false,
		Background: true,
		Sparse:     false,
	}

	err := a.mgoSession.DB("").C("pantahub_devices_tokens").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index owner for pantahub_devices_tokens: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"nick"},
		Unique:     true,
		Background: true,
		Sparse:     false,
	}

	err = a.mgoSession.DB("").C("pantahub_devices_tokens").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index nick for pantahub_devices_tokens: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"disabled"},
		Unique:     false,
		Background: true,
		Sparse:     false,
	}

	err = a.mgoSession.DB("").C("pantahub_devices_tokens").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index disabled for pantahub_devices_tokens: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"prn"},
		Unique:     true,
		Background: true,
		Sparse:     false,
	}

	err = a.mgoSession.DB("").C("pantahub_devices_tokens").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index prn for pantahub_devices_tokens: " + err.Error())
		return err
	}

	index = mgo.Index{
		Key:        []string{"tokensha"},
		Unique:     true,
		Background: true,
		Sparse:     false,
	}

	err = a.mgoSession.DB("").C("pantahub_devices_tokens").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index prn for pantahub_devices_tokens: " + err.Error())
		return err
	}

	return nil
}