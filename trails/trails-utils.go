// Copyright (c) 2017-2023 Pantacor Ltd.
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

package trails

import (
	"context"
	"errors"
	"time"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

func (a *App) isTrailPublic(pctx context.Context, trailID string) (bool, error) {
	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		return false, errors.New("Cannot get collection")
	}

	trail := trailmodels.Trail{}
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	trailObjectID, err := primitive.ObjectIDFromHex(trailID)
	if err != nil {
		return false, err
	}
	err = collTrails.FindOne(ctx, bson.M{
		"_id":     trailObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&trail)

	if err != nil {
		return false, err
	}

	collDevices := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collDevices == nil {
		return false, errors.New("Cannot get collection2")
	}

	device := devices.Device{}
	ctx, cancel = context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	err = collDevices.FindOne(ctx, bson.M{
		"prn":     trail.Device,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)

	if err != nil {
		return false, err
	}

	return device.IsPublic, nil
}
