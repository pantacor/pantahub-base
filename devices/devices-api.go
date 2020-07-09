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
	"math/rand"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/gcapi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/resty.v1"
)

func init() {
	// seed this for petname as dustin dropped our patch upstream... moo
	rand.Seed(time.Now().Unix())
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// ResolveDeviceIDOrNick : Parse DeviceID Or Nick from the given string and return device objectID
func (a *App) ResolveDeviceIDOrNick(owner string, param string) (*primitive.ObjectID, error) {
	mgoid, err := primitive.ObjectIDFromHex(param)
	if err != nil {
		return a.LookupDeviceNick(owner, param)
	}
	return &mgoid, nil
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
func (a *App) LookupDeviceNick(owner string, deviceID string) (*primitive.ObjectID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return nil, errors.New("Error with Database connectivity")
	}
	deviceObject := Device{}

	dev := collection.FindOne(ctx,
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
func (a *App) FindDeviceByID(ID primitive.ObjectID, device *Device) error {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	if collection == nil {
		return errors.New("Error with Database connectivity")
	}

	err := collection.FindOne(ctx, bson.M{
		"_id": ID,
	}).Decode(&device)
	if err != nil {
		return err
	}

	return nil
}
