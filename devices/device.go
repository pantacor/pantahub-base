//
// Copyright (c) 2017-2026 Pantacor Ltd.
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

// GenerateDeviceNick generates a unique device nick using a 3-word petname
// with a 4-character random suffix to avoid collisions on the owner+nick unique index.
func GenerateDeviceNick() string {
	return petname.Generate(3, "_") + "_" + utils.RandStringLower(4)
}

func createDevice(id, secret, owner string) (*Device, error) {
	newDevice := &Device{}

	if id == "" {
		newDevice.ID = primitive.NewObjectID()
	} else {
		ObjectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, err
		}
		newDevice.ID = ObjectID
	}
	newDevice.Prn = "prn:::devices:/" + newDevice.ID.Hex()
	newDevice.Secret = secret
	newDevice.SecretHash = utils.HashSecret(secret)
	newDevice.Owner = owner
	newDevice.UserMeta = utils.BsonQuoteMap(&newDevice.UserMeta)
	newDevice.DeviceMeta = map[string]interface{}{}
	newDevice.TimeCreated = time.Now()
	newDevice.TimeModified = newDevice.TimeCreated
	newDevice.Nick = GenerateDeviceNick()

	return newDevice, nil
}

// save upserts the device, binding the filter to the owner: a fresh id is
// inserted, a re-register by the same owner is updated, and an id already
// owned by someone else fails the filter and hits the duplicate-_id error on
// the upsert insert — the id is client-supplied on /register, so an unbound
// upsert would let anyone overwrite (take over) another owner's device.
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
		bson.M{"_id": device.ID, "owner": device.Owner},
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

// ErrDeviceNotOwned is returned when the device to change does not exist or
// belongs to somebody else. The two are not told apart on purpose.
var ErrDeviceNotOwned = errors.New("device not found")

// PatchUserMeta merges data into the user-meta of one of owner's devices and
// returns what was applied. A nil value removes its key, and nested maps are
// merged key by key, so a patch never drops the siblings of what it touches.
//
// It is the one implementation behind PATCH /devices/:id/user-meta and every
// other way of changing a device's configuration, so that they cannot drift
// apart. owner has to come from the authenticated identity: it is what pins
// the update to the caller's own devices.
func PatchUserMeta(ctx context.Context, mongoClient *mongo.Client, owner string, deviceID primitive.ObjectID, data map[string]interface{}) (map[string]interface{}, error) {
	// Quote the BSON keys first to handle dots in key names (e.g. "lo.ipv4")
	data = utils.BsonQuoteMap(&data)

	collection := mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return nil, errors.New("error with database connectivity")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	setFields := bson.M{}
	unsetFields := bson.M{}

	// Deep flatten the quoted data to allow atomic nested updates
	flattenMap("user-meta", data, setFields, unsetFields)

	// Always update timemodified
	setFields["timemodified"] = time.Now()

	updateDoc := bson.M{}
	if len(setFields) > 0 {
		updateDoc["$set"] = setFields
	}
	if len(unsetFields) > 0 {
		updateDoc["$unset"] = unsetFields
	}

	updateResult, err := collection.UpdateOne(ctx, bson.M{"_id": deviceID, "owner": owner}, updateDoc)
	if err != nil {
		return nil, err
	}
	if updateResult.MatchedCount == 0 {
		return nil, ErrDeviceNotOwned
	}

	return utils.BsonUnquoteMap(&data), nil
}

// flattenMap flattens a nested map into dot-notation keys for MongoDB atomic updates
func flattenMap(prefix string, m map[string]interface{}, setFields bson.M, unsetFields bson.M) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		if v == nil {
			unsetFields[key] = ""
			continue
		}

		// If it's a map, recurse to flatten further
		// We handle both map[string]interface{} and bson.M
		if nestedMap, ok := v.(map[string]interface{}); ok {
			flattenMap(key, nestedMap, setFields, unsetFields)
		} else if nestedMap, ok := v.(bson.M); ok {
			flattenMap(key, map[string]interface{}(nestedMap), setFields, unsetFields)
		} else {
			// BSON-quote the leaf key part to handle dots in the metadata key name
			// while preserving the path dots used for deep update.
			setFields[key] = v
		}
	}
}
