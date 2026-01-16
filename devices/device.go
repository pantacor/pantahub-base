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
	"errors"
	"strconv"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
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

func (device *Device) save(ctx context.Context, collection *mongo.Collection) (*mongo.UpdateResult, error) {
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	result, err := collection.UpdateOne(
		ctxC,
		bson.M{"_id": device.ID},
		bson.M{"$set": device},
		updateOptions,
	)

	return result, err
}

// GetDeviceByID get device using string ID
func GetDeviceByID(ctx context.Context, id string, collection *mongo.Collection) (*Device, error) {
	var device Device
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}

	mgoid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = collection.FindOne(ctxC,
		bson.M{
			"_id":     mgoid,
			"garbage": bson.M{"$ne": true},
		}).
		Decode(&device)

	return &device, err
}

// ErrDeviceQuotaExceeded is returned when the device quota is exceeded
var ErrDeviceQuotaExceeded = errors.New("device quota exceeded")

// DeviceQuotaResult contains the result of a device quota check
type DeviceQuotaResult struct {
	CurrentCount int64
	MaxAllowed   int64
	Exceeded     bool
}

// CheckDeviceQuota checks if the owner has exceeded their device quota
func CheckDeviceQuota(
	ctx context.Context,
	owner string,
	mongoClient *mongo.Client,
	subService subscriptions.SubscriptionService,
) (*DeviceQuotaResult, error) {
	result := &DeviceQuotaResult{}

	// Get subscription for the owner
	sub, err := subService.LoadBySubject(ctx, utils.Prn(owner))
	if err != nil {
		// If no subscription found, use default (FREE tier)
		sub = subService.GetDefaultSubscription(utils.Prn(owner))
	}

	// Get device quota from subscription
	deviceQuotaVal := sub.GetProperty("DEVICES")
	if deviceQuotaVal == nil {
		// No device quota set, allow unlimited
		result.MaxAllowed = -1
		return result, nil
	}

	deviceQuotaStr, ok := deviceQuotaVal.(string)
	if !ok {
		return nil, errors.New("invalid device quota value in subscription")
	}

	maxDevices, err := strconv.ParseInt(deviceQuotaStr, 10, 64)
	if err != nil {
		return nil, errors.New("invalid device quota value: " + err.Error())
	}

	// If quota is 0 or negative, treat as unlimited
	if maxDevices <= 0 {
		result.MaxAllowed = -1
		return result, nil
	}

	result.MaxAllowed = maxDevices

	// Count current devices for the owner
	collection := mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	deviceCount, err := collection.CountDocuments(ctxC, bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	})
	if err != nil {
		return nil, errors.New("error counting devices: " + err.Error())
	}

	result.CurrentCount = deviceCount
	result.Exceeded = deviceCount >= maxDevices

	if utils.GetEnv(utils.EnvPantahubDisableQuota) == "true" {
		result.Exceeded = false
	}

	return result, nil
}
