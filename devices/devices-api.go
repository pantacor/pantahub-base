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

package devices

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/gcapi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/resty.v1"
)

func init() {
	// seed this for petname as dustin dropped our patch upstream... moo
	rand.Seed(time.Now().Unix())
}

func (app *App) EnsureDevicesIndices() error {
	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(CreateIndexTimeout)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: "nick", Value: int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(CreateIndexTimeout)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bson.D{
			{Key: "timemodified", Value: int32(1)},
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
	CreateIndexesOptions.SetMaxTime(CreateIndexTimeout)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bson.D{
			{Key: "prn", Value: int32(1)},
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
	CreateIndexesOptions.SetMaxTime(CreateIndexTimeout)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
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
	CreateIndexesOptions.SetMaxTime(CreateIndexTimeout)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bson.D{
			{Key: "device", Value: int32(1)},
			{Key: "garbage", Value: int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_devices: " + err.Error())
		return nil
	}

	return nil
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// ResolveDeviceIDOrNick : Parse DeviceID Or Nick from the given string and return device objectID
func (a *App) ResolveDeviceIDOrNick(ctx context.Context, owner string, param string) (*primitive.ObjectID, error) {
	mgoid, err := primitive.ObjectIDFromHex(param)
	if err != nil {
		return a.LookupDeviceNick(ctx, owner, param)
	}
	return &mgoid, nil
}

// MarkDeviceAsGarbage : Mark Device as Garbage
func MarkDeviceAsGarbage(
	deviceID string,
) (
	gcapi.MarkDeviceGarbage,
	*resty.Response,
	error,
) {
	response := gcapi.MarkDeviceGarbage{}
	APIEndPoint := utils.GetEnv("PANTAHUB_GC_API") + "/markgarbage/device/" + deviceID
	res, err := resty.R().Put(APIEndPoint)
	if err != nil {
		return response, nil, err
	}
	err = json.Unmarshal(res.Body(), &response)
	return response, res, err
}

// LookupDeviceNick : Lookup Device Nicks and return device id
func (a *App) LookupDeviceNick(ctx context.Context, owner string, deviceID string) (*primitive.ObjectID, error) {
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}
	deviceObject := Device{}

	dev := collection.FindOne(ctxC,
		bson.M{
			"owner":   owner,
			"nick":    deviceID,
			"garbage": bson.M{"$ne": true},
		},
	)

	err := dev.Decode(&deviceObject)
	if err != nil {
		return nil, err
	}
	return &deviceObject.ID, nil
}

// FindDeviceByID finds the device by id
func (a *App) FindDeviceByID(ctx context.Context, ID primitive.ObjectID, device *Device) error {

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return errors.New("Error with Database connectivity")
	}

	err := collection.FindOne(ctxC, bson.M{
		"_id": ID,
	}).Decode(&device)
	if err != nil {
		return err
	}

	return nil
}
