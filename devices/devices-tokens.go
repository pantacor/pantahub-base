//
// Copyright 2018-2019  Pantacor Ltd.
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
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"

	petname "github.com/dustinkirkland/golang-petname"
	"gopkg.in/mgo.v2/bson"
)

type PantahubDevicesJoinToken struct {
	Id              primitive.ObjectID     `json:"id" bson:"_id"`
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
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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
		utils.RestErrorWrapper(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return

	}

	req := PantahubDevicesJoinToken{}

	err := r.DecodeJsonPayload(&req)

	if err != nil && err != rest.ErrJsonPayloadEmpty {
		utils.RestErrorWrapper(w, "error decoding request: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Id = primitive.NewObjectID()
	req.Prn = utils.IdGetPrn(req.Id, "devices-tokens")

	if req.Nick == "" {
		req.Nick = petname.Generate(3, "_")
	}

	req.Owner = caller.(string)

	key := make([]byte, 24)

	_, err = rand.Read(key)
	if err != nil {
		utils.RestErrorWrapper(w, "error generating random token: "+err.Error(), http.StatusInternalServerError)
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

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(ctx, &req)

	if err != nil {
		utils.RestErrorWrapper(w, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
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
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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
		utils.RestErrorWrapper(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	r.ParseForm()
	tokenId := r.PathParam("id")
	tokenIdBson := bson.ObjectIdHex(tokenId)

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id":   tokenIdBson,
			"owner": caller.(string),
		},
		bson.M{"$set": bson.M{"disabled": true}},
		updateOptions,
	)

	if err != nil {
		utils.RestErrorWrapper(w, "error inserting device token into database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(bson.M{"status": "OK"})
}

func (a *DevicesApp) handle_gettokens(w rest.ResponseWriter, r *rest.Request) {

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Missing JWT_PAYLOAD", http.StatusBadRequest)
		return
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
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
		utils.RestErrorWrapper(w, "Can only be updated by Device: handle_posttoken", http.StatusBadRequest)
		return
	}

	res := []PantahubDevicesJoinToken{}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	findOptions := options.Find()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, bson.M{
		"owner": caller.(string),
	}, findOptions)

	if err != nil {
		utils.RestErrorWrapper(w, "error getting device tokens for user:"+err.Error(), http.StatusForbidden)
		return
	}

	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := PantahubDevicesJoinToken{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		// lets not reveal details about token when collection gets queried
		result.TokenSha = nil
		result.Token = ""
		res = append(res, result)
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

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = col.FindOne(ctx, bson.M{"tokensha": tokenSha}).Decode(&res)
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

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
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
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
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
			{Key: "disabled", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "tokensha", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	return nil
}
