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

package profiles

import (
	"context"

	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Profile : Public information for one account
type Profile struct {
	ID      primitive.ObjectID `json:"-" bson:"_id"`
	Nick    string             `json:"nick" bson:"-"`
	Bio     string             `json:"bio" bson:"bio"`
	Public  bool               `json:"public" bson:"public"`
	Garbage bool               `json:"garbage" bson:"garbage"`

	TimeCreated  time.Time `json:"time-created" bson:"time-created"`
	TimeModified time.Time `json:"time-modified" bson:"time-modified"`
}

// handleGetProfile Get a user profile by user ID
// @Summary Get a user profile by user ID
// Public/Private Profile access logic:
// 1.Check if the user have an active profile or not.
// 2.Check if the user have Public devices or not
// 3.If (1) is FALSE && (2) is TRUE then create a private profile for the user.
// 4.If the user have private profile but have public devices then return only "nick" field as api response.
// 5.If the user have private profile and have no public devices then return error api response.
// 6.If the user have public profile but have no public devices then Mark the profile as private and return error api response
// 7.if the user have public profile then return all the profile details as api response
// @Description Get a user profile by user ID
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags user
// @Param id path string true "ID"
// @Success 200 {array} Profile
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /{id} [get]
func (a *App) handleGetProfile(w rest.ResponseWriter, r *rest.Request) {
	var profile Profile
	userID, err := primitive.ObjectIDFromHex(r.PathParam("id"))
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid ID", http.StatusNotFound)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	haveProfile, err := a.ExistsInProfiles(userID)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
		return
	}
	havePublicDevices, err := a.HavePublicDevices(userID)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
		return
	}
	// Make a new private profile if user have no profile & have public devices
	if !haveProfile && havePublicDevices {
		err := a.MakeUserProfile(userID)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx,
		bson.M{
			"_id": userID,
		}).Decode(&profile)
	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}
	account, err := a.getUserAccount(userID)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
		return
	}
	profile.Nick = account.Nick

	if !profile.Public && havePublicDevices {
		//User have private profile & have public devices
		response := map[string]interface{}{
			"nick": profile.Nick,
		}
		w.WriteJson(response)

	} else if !profile.Public && !havePublicDevices {
		//User have private profile & have no public devices
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return

	} else if profile.Public && !havePublicDevices {
		//User have public profile & have no public devices
		//Mark profile as Private
		err := a.MarkProfileAsPrivate(profile.ID)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
			return
		}
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	} else if profile.Public {
		w.WriteJson(profile)
	}
}

// ExistsInProfiles : Check if a user details exists in profiles or not
func (a *App) ExistsInProfiles(ID primitive.ObjectID) (bool, error) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count, err := collection.CountDocuments(ctx,
		bson.M{
			"_id": ID,
		})
	if err != nil {
		return false, err
	}
	return count > 0, err
}

// HavePublicDevices : Check if a user have public devices or not
func (a *App) HavePublicDevices(ID primitive.ObjectID) (bool, error) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	account, err := a.getUserAccount(ID)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count, err := collection.CountDocuments(ctx,
		bson.M{
			"owner":    account.Prn,
			"ispublic": true,
			"garbage":  false,
		})
	if err != nil {
		return false, err
	}
	return count > 0, err
}

// MakeUserProfile : Make User Profile
func (a *App) MakeUserProfile(ID primitive.ObjectID) error {
	account, err := a.getUserAccount(ID)
	if err != nil {
		return err
	}
	profile := Profile{
		ID:           account.ID,
		TimeCreated:  account.TimeCreated,
		TimeModified: account.TimeModified,
		Public:       false,
		Garbage:      false,
	}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(ctx, &profile)
	if err != nil {
		return err
	}
	return nil
}

// MakeUserProfile : Make User Profile
func (a *App) getUserAccount(ID primitive.ObjectID) (accounts.Account, error) {

	var account accounts.Account
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := collection.FindOne(ctx,
		bson.M{
			"_id": ID,
		}).Decode(&account)
	if err != nil {
		return account, err
	}
	return account, err
}

// MarkProfileAsPrivate : Mark Profile As Private
func (a *App) MarkProfileAsPrivate(ID primitive.ObjectID) error {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": ID},
		bson.M{"$set": bson.M{"public": false}},
		nil,
	)
	if err != nil {
		return err
	}
	return nil
}
