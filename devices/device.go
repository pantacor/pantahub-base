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
	"errors"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

func createDevice(id, secret, owner string) (*Device, error) {
	newDevice := &Device{}

	mgoid := bson.ObjectIdHex(id)
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		return nil, err
	}
	newDevice.ID = ObjectID
	newDevice.Prn = "prn:::devices:/" + newDevice.ID.Hex()
	newDevice.Secret = secret
	newDevice.Owner = owner
	newDevice.UserMeta = utils.BsonQuoteMap(&newDevice.UserMeta)
	newDevice.DeviceMeta = map[string]interface{}{}
	newDevice.TimeCreated = time.Now()
	newDevice.TimeModified = newDevice.TimeCreated
	newDevice.Nick = petname.Generate(3, "_")

	return newDevice, nil
}

func (device *Device) save(collection *mongo.Collection) (*mongo.UpdateResult, error) {
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	result, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": device.ID},
		bson.M{"$set": device},
		updateOptions,
	)

	return result, err
}

// GetDeviceByID get device using string ID
func GetDeviceByID(id string, collection *mongo.Collection) (*Device, error) {
	var device Device
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}

	mgoid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collection.FindOne(ctx,
		bson.M{
			"_id":     mgoid,
			"garbage": bson.M{"$ne": true},
		}).
		Decode(&device)

	return &device, err
}
