//
// Copyright 2020  Pantacor Ltd.
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
	"net/http"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/profiles"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo/options"

	"gopkg.in/mgo.v2/bson"
)

// handleGetDevices Get all accounts devices
// @Summary Get all accounts devices
// Get Any user's public devices by using owner/ owner-nick params
// Eg:
//
//	GET /devices/?owner-nick=asac
//	GET /devices/?owner=prn:pantahub.com:auth:/5e1875e2fb13950bc38d0ebd
//
// @Description Get all accounts devices
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param owner-nick query string false "Owner nick"
// @Param owner query string false "Owner PRN"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices [get]
func (a *App) handleGetDevices(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER or DEVICE"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER or DEVICE"

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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	query := bson.M{
		"garbage": bson.M{"$ne": true},
	}
	ownerValue, ok1 := r.URL.Query()["owner"]
	ownerNickvalue, ok2 := r.URL.Query()["owner-nick"]
	if ok1 {
		// To get devices of any user who have public devices
		ok, err := utils.ValidateUserPrn(ownerValue[0])
		if err != nil || !ok {
			utils.RestErrorWrapper(w, "Invalid owner prn", http.StatusForbidden)
			return
		}
		query["owner"] = ownerValue[0]

		if ownerValue[0] != owner {
			query["ispublic"] = true
		}

	} else if ok2 {
		//To get devices of any user who have public devices by using owner nick
		account, err := a.GetUserAccountByNick(r.Context(), ownerNickvalue[0])
		if err != nil {
			utils.RestErrorWrapper(w, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
			return
		}

		query["owner"] = account.Prn

		if account.Prn != owner {
			query["ispublic"] = true
		}

	} else {
		if authType == "USER" || authType == "SESSION" {
			query["owner"] = owner
		} else {
			query["prn"] = owner
		}
	}

	for k, v := range r.URL.Query() {
		if k == "owner-nick" {
			continue
		}
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

		// If token owner (device token or account token)
		// is not the same as Owner in account token case
		// or is not the same as Prn in device token case
		if owner != result.Owner && owner != result.Prn {
			result.Challenge = ""
			result.Secret = ""
			result.UserMeta = map[string]interface{}{}
			result.DeviceMeta = map[string]interface{}{}
		}
		devices = append(devices, result)
	}

	w.WriteJson(devices)
}

// handleGetDevice Get a device using the device ID or the PRN or the device Nick
// @Summary Get a device using the device ID or the PRN or the device Nick
// @Description Get a device using the device ID or the PRN or the device Nick
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|Nick|PRN"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id} [get]
func (a *App) handleGetDevice(w rest.ResponseWriter, r *rest.Request) {
	var device Device

	authID, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	ownerPtr := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if ownerPtr == nil {
		ownerPtr = authID
	}

	owner, ok := ownerPtr.(string)
	if !ok {
		utils.RestErrorWrapper(w, "Session has no owner info", http.StatusBadRequest)
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

	value, useOtherOwnerPrn := r.URL.Query()["owner"]
	if useOtherOwnerPrn {
		ok, err := utils.ValidateUserPrn(value[0])
		if err != nil || !ok {
			utils.RestErrorWrapper(w, "Invalid owner prn", http.StatusForbidden)
			return
		}
		owner = value[0]
	}

	value, useOtherOwnerNick := r.URL.Query()["owner-nick"]
	if useOtherOwnerNick {
		account, err := a.GetUserAccountByNick(r.Context(), value[0])
		if err != nil {
			utils.RestErrorWrapper(w, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
			return
		}
		owner = account.Prn
	}

	mgoid, err := a.ResolveDeviceIDOrNick(r.Context(), owner, r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
		return
	}

	query := bson.M{
		"_id":     mgoid,
		"garbage": bson.M{"$ne": true},
	}

	// To fetch other user's public device
	if useOtherOwnerPrn || useOtherOwnerNick {
		query["owner"] = owner
		if owner != ownerPtr.(string) { // only if requesting user!= custom owner param value
			query["ispublic"] = true
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, query).Decode(&device)
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
		ownerAccount, ok := accountsdata.DefaultAccounts[device.Owner]
		if !ok {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			err := collectionAccounts.FindOne(ctx,
				bson.M{"prn": device.Owner}).
				Decode(&ownerAccount)

			if err != nil {
				utils.RestErrorWrapper(w, "Owner account not Found", http.StatusInternalServerError)
				return
			}
		}

		profileMeta, _ := a.getProfileMetaData(r.Context(), device.Owner)
		device.UserMeta = utils.MergeMaps(profileMeta, device.UserMeta)
		device.OwnerNick = ownerAccount.Nick
	}

	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	w.WriteJson(device)
}

// GetUserAccountByNick : Get User Account By Nick
func (a *App) GetUserAccountByNick(parentCtx context.Context, nick string) (accounts.Account, error) {

	var account accounts.Account

	account, ok := accountsdata.DefaultAccounts["prn:pantahub.com:auth:/"+nick]
	if !ok {

		collectionAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
		defer cancel()
		err := collectionAccounts.FindOne(ctx,
			bson.M{"nick": nick}).
			Decode(&account)

		if err != nil {
			return account, err
		}
	}
	return account, nil
}

func (a *App) getProfileMetaData(parentCtx context.Context, prn string) (map[string]interface{}, error) {
	profile := &profiles.Profile{
		Meta: map[string]interface{}{},
	}
	queryOptions := options.FindOneOptions{}
	queryOptions.Projection = bson.M{
		"meta": 1,
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	err := collection.FindOne(ctx, bson.M{"prn": prn}, &queryOptions).Decode(profile)
	return profile.Meta, err
}
