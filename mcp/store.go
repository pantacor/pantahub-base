//
// Copyright 2026 Pantacor Ltd.
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

package mcp

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	devicesCollection = "pantahub_devices"
	stepsCollection   = "pantahub_steps"

	// queryTimeout bounds the Mongo work of one tool call, like the REST
	// handlers bound theirs.
	queryTimeout = 10 * time.Second

	// statusDone is the progress status of a revision the device runs.
	statusDone = "DONE"
)

// errNotFound covers both "does not exist" and "is not yours": a tool must not
// let a caller probe for other accounts' devices.
var errNotFound = errors.New("not found")

// store is the data access of the tools. Every query is pinned to the owner it
// is given, which the tools always take from the verified token; nothing here
// reads an owner from tool arguments.
type store struct {
	mongoClient *mongo.Client
	logsApp     *logs.App
}

func (s *store) collection(name string) *mongo.Collection {
	return s.mongoClient.Database(utils.MongoDb).Collection(name)
}

// listDevices pages through the owner's devices in _id order. after is the id
// of the last device of the previous page. It returns one page and whether
// more devices follow.
func (s *store) listDevices(ctx context.Context, owner, nickPrefix, after string, limit int64) ([]devices.Device, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}
	if nickPrefix != "" {
		query["nick"] = bson.M{"$regex": "^" + regexp.QuoteMeta(nickPrefix), "$options": "i"}
	}
	if after != "" {
		afterID, err := primitive.ObjectIDFromHex(after)
		if err != nil {
			return nil, false, errors.New("invalid cursor")
		}
		query["_id"] = bson.M{"$gt": afterID}
	}

	// The metadata maps can be large and a listing does not show them.
	findOptions := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(limit + 1).
		SetProjection(bson.M{"user-meta": 0, "device-meta": 0})

	cur, err := s.collection(devicesCollection).Find(ctx, query, findOptions)
	if err != nil {
		return nil, false, err
	}
	defer cur.Close(ctx)

	result := make([]devices.Device, 0, limit)
	if err := cur.All(ctx, &result); err != nil {
		return nil, false, err
	}

	more := int64(len(result)) > limit
	if more {
		result = result[:limit]
	}
	return result, more, nil
}

// resolveDevice finds one of the owner's devices by id, PRN or nick.
func (s *store) resolveDevice(ctx context.Context, owner, ref string) (*devices.Device, error) {
	device, err := s.findDevice(ctx, owner, ref)
	if err != nil {
		return nil, err
	}
	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)
	return device, nil
}

// getDevice is resolveDevice with user-meta as GET /devices/:id serves it,
// the owner's global profile meta included.
func (s *store) getDevice(ctx context.Context, owner, ref string) (*devices.Device, error) {
	device, err := s.findDevice(ctx, owner, ref)
	if err != nil {
		return nil, err
	}
	device.UserMeta = devices.EffectiveUserMeta(ctx, s.mongoClient, device.Owner, device.UserMeta)
	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)
	return device, nil
}

// findDevice returns the device document as stored, its maps still quoted.
func (s *store) findDevice(ctx context.Context, owner, ref string) (*devices.Device, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("device is required")
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := bson.M{
		"owner":   owner,
		"garbage": bson.M{"$ne": true},
	}
	if id, ok := devices.ParseDeviceRef(ref); ok {
		query["_id"] = id
	} else {
		query["nick"] = ref
	}

	device := &devices.Device{}
	err := s.collection(devicesCollection).FindOne(ctx, query).Decode(device)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return device, nil
}

// listSteps returns the revisions of a device newest first, without their state
// and metadata. beforeRev pages backwards; zero or less starts at the newest.
// A device's trail shares the device's id.
func (s *store) listSteps(ctx context.Context, owner string, deviceID primitive.ObjectID, beforeRev int, limit int64) ([]trailmodels.Step, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := bson.M{
		"trail-id": deviceID,
		"owner":    owner,
		"garbage":  bson.M{"$ne": true},
	}
	if beforeRev > 0 {
		query["rev"] = bson.M{"$lt": beforeRev}
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "rev", Value: -1}}).
		SetLimit(limit + 1).
		SetProjection(bson.M{"state": 0, "meta": 0, "used_objects": 0, "progress.logs": 0, trailmodels.ProgressLogField: 0})

	cur, err := s.collection(stepsCollection).Find(ctx, query, findOptions)
	if err != nil {
		return nil, false, err
	}
	defer cur.Close(ctx)

	result := make([]trailmodels.Step, 0, limit)
	if err := cur.All(ctx, &result); err != nil {
		return nil, false, err
	}

	more := int64(len(result)) > limit
	if more {
		result = result[:limit]
	}
	return result, more, nil
}

// getStep returns one revision of a device in full. A negative rev means the
// newest one, and a non-empty status the newest one in that status.
func (s *store) getStep(ctx context.Context, owner string, deviceID primitive.ObjectID, rev int, status string) (*trailmodels.Step, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := bson.M{
		"trail-id": deviceID,
		"owner":    owner,
		"garbage":  bson.M{"$ne": true},
	}
	if rev >= 0 {
		query["rev"] = rev
	}
	if status != "" {
		query["progress.status"] = status
	}

	step := &trailmodels.Step{}
	err := s.collection(stepsCollection).
		FindOne(ctx, query, options.FindOne().SetSort(bson.D{{Key: "rev", Value: -1}})).
		Decode(step)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}

	step.State = utils.BsonUnquoteMap(&step.State)
	step.Meta = utils.BsonUnquoteMap(&step.Meta)
	return step, nil
}

// getLogs queries the log store for one device of the owner.
func (s *store) getLogs(ctx context.Context, q logs.Query) (*logs.Pager, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	return s.logsApp.GetLogs(ctx, q)
}

// patchUserMeta merges data into the configuration of one of the owner's
// devices. It goes through devices.PatchUserMeta, the implementation behind the
// REST endpoint, so both change a device in exactly the same way.
func (s *store) patchUserMeta(ctx context.Context, owner string, deviceID primitive.ObjectID, data map[string]interface{}) error {
	_, err := devices.PatchUserMeta(ctx, s.mongoClient, owner, deviceID, data)
	if errors.Is(err, devices.ErrDeviceNotOwned) {
		return errNotFound
	}
	return err
}

// maxListedTokens bounds a token listing, so a tool answer stays small.
// Accounts hold a handful.
const maxListedTokens = 200

// listDeviceTokens returns the owner's active device join tokens, like
// GET /devices/tokens does.
func (s *store) listDeviceTokens(ctx context.Context, owner string) ([]utils.PantahubDevicesJoinToken, error) {
	return devices.ListJoinTokens(ctx, s.mongoClient, owner, maxListedTokens)
}

func deviceTokenID(ref string) (primitive.ObjectID, error) {
	ref = strings.TrimSpace(ref)
	ref = ref[strings.LastIndex(ref, "/")+1:] // accept the token's PRN too
	id, err := primitive.ObjectIDFromHex(ref)
	if err != nil {
		return primitive.NilObjectID, errNotFound
	}
	return id, nil
}

// getDeviceToken returns one of the owner's active device join tokens.
func (s *store) getDeviceToken(ctx context.Context, owner, ref string) (*utils.PantahubDevicesJoinToken, error) {
	id, err := deviceTokenID(ref)
	if err != nil {
		return nil, err
	}
	return joinTokenResult(devices.GetJoinToken(ctx, s.mongoClient, owner, id))
}

// updateDeviceToken renames one of the owner's active device join tokens and
// or replaces the configuration devices enrolled with it start out with,
// through the same code as PATCH /devices/tokens/:id.
func (s *store) updateDeviceToken(ctx context.Context, owner, ref string, nick *string, defaultUserMeta map[string]interface{}) (*utils.PantahubDevicesJoinToken, error) {
	id, err := deviceTokenID(ref)
	if err != nil {
		return nil, err
	}
	return joinTokenResult(devices.PatchJoinToken(ctx, s.mongoClient, owner, id,
		devices.JoinTokenPatch{Nick: nick, DefaultUserMeta: defaultUserMeta}))
}

func joinTokenResult(token *utils.PantahubDevicesJoinToken, err error) (*utils.PantahubDevicesJoinToken, error) {
	if errors.Is(err, devices.ErrJoinTokenNotFound) {
		return nil, errNotFound
	}
	return token, err
}

// listApps returns the OAuth applications the owner registered.
func (s *store) listApps(ctx context.Context, owner string) ([]apps.TPApp, error) {
	return apps.SearchApps(ctx, owner, "", s.mongoClient.Database(utils.MongoDb))
}

// getApp returns one of the owner's applications by id, nick or client id.
func (s *store) getApp(ctx context.Context, owner, ref string) (*apps.TPApp, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("app is required")
	}
	app, _, err := apps.SearchApp(ctx, owner, ref, s.mongoClient.Database(utils.MongoDb))
	if err != nil || app == nil {
		return nil, errNotFound
	}
	return app, nil
}

// updateApp changes the display name and callback URLs of one of the owner's
// applications. See apps.UpdateDetails for why nothing else can be changed.
func (s *store) updateApp(ctx context.Context, owner, ref string, details apps.Details) (*apps.TPApp, error) {
	app, err := apps.UpdateDetails(ctx, s.mongoClient.Database(utils.MongoDb), owner, strings.TrimSpace(ref), details)
	if errors.Is(err, apps.ErrAppNotOwned) {
		return nil, errNotFound
	}
	return app, err
}
