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
	"net/http"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/profiles"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/mongo"
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
func (a *App) handleGetDevices(c *echo.Context) error {
	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER or DEVICE"

		return echoutil.WriteJSON(c, int(err.Code), err)
	}

	var authType accounts.AccountType
	authTypeValue, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER or DEVICE"

		return echoutil.WriteJSON(c, int(err.Code), err)
	} else {
		authType = accounts.AccountType(authTypeValue.(string))
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	devices := make([]Device, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	query := bson.M{
		"garbage": bson.M{"$ne": true},
	}
	ownerValue, ok1 := c.Request().URL.Query()["owner"]
	ownerNickvalue, ok2 := c.Request().URL.Query()["owner-nick"]
	if ok1 {
		// To get devices of any user who have public devices
		ok, err := utils.ValidateUserPrn(ownerValue[0])
		if err != nil || !ok {
			return echoutil.RestErrorWrapper(c, "Invalid owner prn", http.StatusForbidden)
		}
		query["owner"] = ownerValue[0]

		if ownerValue[0] != owner {
			query["ispublic"] = true
		}

	} else if ok2 {
		// To get devices of any user who have public devices by using owner nick
		account, err := a.GetUserAccountByNick(c.Request().Context(), ownerNickvalue[0])
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
		}

		query["owner"] = account.Prn

		if account.Prn != owner {
			query["ispublic"] = true
		}

	} else {
		if authType == accounts.AccountTypeUser || authType == accounts.AccountTypeSessionUser {
			query["owner"] = owner
		} else {
			query["prn"] = owner
		}
	}

	othersDevices := query["ispublic"] == true
	for k, v := range c.Request().URL.Query() {
		if k == "owner-nick" {
			continue
		}
		if strings.HasPrefix(k, "$") {
			return echoutil.RestErrorWrapper(c, "Invalid filter: "+k, http.StatusBadRequest)
		}
		// Filtering would reveal what the response hides from non-owners.
		if othersDevices && isOwnerOnlyField(k) {
			return echoutil.RestErrorWrapper(c, "Invalid filter: "+k, http.StatusBadRequest)
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
		return echoutil.RestErrorWrapper(c, "Error on fetching devices:"+err.Error(), http.StatusForbidden)
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := Device{}
		err := cur.Decode(&result)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
		}

		result.UserMeta = utils.BsonUnquoteMap(&result.UserMeta)
		result.DeviceMeta = utils.BsonUnquoteMap(&result.DeviceMeta)

		// If token owner (device token or account token)
		// is not the same as Owner in account token case
		// or is not the same as Prn in device token case
		if owner != result.Owner && owner != result.Prn {
			result = result.PublicView()
		}
		devices = append(devices, result)
	}

	return echoutil.WriteJSON(c, http.StatusOK, devices)
}

// isOwnerOnlyField tells whether a device field is withheld from non-owners.
func isOwnerOnlyField(field string) bool {
	root := strings.SplitN(field, ".", 2)[0]
	switch root {
	case "user-meta", "device-meta", "secret", "secret_hash", "challenge":
		return true
	}
	return false
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
func (a *App) handleGetDevice(c *echo.Context) error {
	var device Device

	authID, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	ownerPtr := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["owner"]
	if ownerPtr == nil {
		ownerPtr = authID
	}

	owner, ok := ownerPtr.(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Session has no owner info", http.StatusBadRequest)
	}

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in with a known authentication type.", http.StatusForbidden)
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
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	collectionAccounts := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collectionAccounts == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database (accounts) connectivity", http.StatusInternalServerError)
	}

	value, useOtherOwnerPrn := c.Request().URL.Query()["owner"]
	if useOtherOwnerPrn {
		ok, err := utils.ValidateUserPrn(value[0])
		if err != nil || !ok {
			return echoutil.RestErrorWrapper(c, "Invalid owner prn", http.StatusForbidden)
		}
		owner = value[0]
	}

	value, useOtherOwnerNick := c.Request().URL.Query()["owner-nick"]
	if useOtherOwnerNick {
		account, err := a.GetUserAccountByNick(c.Request().Context(), value[0])
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
		}
		owner = account.Prn
	}

	mgoid, err := a.ResolveDeviceIDOrNick(c.Request().Context(), owner, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error Parsing Device ID or Nick:"+err.Error(), http.StatusBadRequest)
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

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// Start a session with causal consistency to ensure read-after-write consistency
	session, err := a.mongoClient.StartSession()
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error starting database session: "+err.Error(), http.StatusInternalServerError)
	}
	defer session.EndSession(ctx)

	err = mongo.WithSession(ctx, session, func(sc mongo.SessionContext) error {
		return collection.FindOne(sc, query).Decode(&device)
	})
	if err != nil && mongoutils.IsNotFound(err) {
		return echoutil.RestErrorWrapper(c, "Device not found", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	if !device.IsPublic {
		// XXX: fixme; needs delegation of authorization for device accessing its resources
		// could be subscriptions, but also something else
		if callerIsDevice && device.Prn != authID {
			return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
		}

		if callerIsUser && device.Owner != authID {
			return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
		}
	}

	// Only the owner and the device itself see more than the public view.
	canSeeMeta := authID == device.Prn || authID == device.Owner

	if device.Owner != "" {
		var ownerAccount accounts.Account

		// first check default accounts like user1, user2, etc...
		ownerAccount, ok := accountsdata.DefaultAccounts[device.Owner]
		if !ok {
			ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
			defer cancel()
			err := collectionAccounts.FindOne(ctx,
				bson.M{"prn": device.Owner}).
				Decode(&ownerAccount)
			if err != nil {
				return echoutil.RestErrorWrapper(c, "Owner account not Found", http.StatusInternalServerError)
			}
		}

		if canSeeMeta {
			device.UserMeta = EffectiveUserMeta(c.Request().Context(), a.mongoClient, device.Owner, device.UserMeta)
		}
		device.OwnerNick = ownerAccount.Nick
	}

	if !canSeeMeta {
		return echoutil.WriteJSON(c, http.StatusOK, device.PublicView())
	}

	device.Secret = ""
	device.UserMeta = utils.BsonUnquoteMap(&device.UserMeta)
	device.DeviceMeta = utils.BsonUnquoteMap(&device.DeviceMeta)

	return echoutil.WriteJSON(c, http.StatusOK, device)
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

// EffectiveUserMeta is the configuration a device of owner runs with: the
// owner's global profile meta with the device's own user-meta on top. Both are
// in stored (BSON-quoted) form, so merge before unquoting.
func EffectiveUserMeta(parentCtx context.Context, mongoClient *mongo.Client, owner string, deviceUserMeta map[string]interface{}) map[string]interface{} {
	profile := &profiles.Profile{
		Meta: map[string]interface{}{},
	}
	queryOptions := options.FindOneOptions{}
	queryOptions.Projection = bson.M{
		"meta": 1,
	}

	collection := mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	// An account without a profile has no global meta.
	_ = collection.FindOne(ctx, bson.M{"prn": owner}, &queryOptions).Decode(profile)
	return utils.MergeMaps(profile.Meta, deviceUserMeta)
}
