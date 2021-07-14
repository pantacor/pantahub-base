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
	"time"

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

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

func (a *App) getProfile(prn string, projection map[string]interface{}) (*Profile, error) {
	profile := &Profile{}
	queryOptions := options.FindOneOptions{}
	if projection != nil {
		queryOptions.Projection = utils.MergeDefaultProjection(projection)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := collection.FindOne(ctx, bson.M{"prn": prn}, &queryOptions).Decode(profile)
	return profile, err
}

// HavePublicDevices : Check if a user have public devices or not
func (a *App) HavePublicDevices(prn string) (bool, error) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := collection.CountDocuments(ctx,
		bson.M{
			"owner":    prn,
			"ispublic": true,
			"garbage":  false,
		})
	if err != nil {
		return false, err
	}
	return count > 0, err
}

// MakeUserProfile : Make User Profile from account
func (a *App) MakeUserProfile(account *accounts.Account, newProfile *UpdateableProfile) (*Profile, error) {
	profile := &Profile{
		UpdateableProfile: newProfile,
		ID:                account.ID,
		Nick:              account.Nick,
		Prn:               account.Prn,
		TimeCreated:       account.TimeCreated,
		TimeModified:      account.TimeModified,
		Public:            true,
		Garbage:           false,
	}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := collection.InsertOne(ctx, &profile)

	return profile, err
}

// getUserAccount get user account using account by something (default: by nick)
func (a *App) getUserAccount(accountNick string, by string) (*accounts.Account, error) {
	if by == "" {
		by = "nick"
	}
	account := &accounts.Account{}
	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	search := bson.M{}
	search[by] = accountNick
	err := col.FindOne(ctx, search).Decode(account)

	return account, err
}

// MarkProfileAsPrivate : Mark Profile As Private
func (a *App) MarkProfileAsPrivate(prn string) error {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"prn": prn},
		bson.M{"$set": bson.M{"public": false}},
		nil,
	)
	if err != nil {
		return err
	}
	return nil
}

func (a *App) updateProfile(accountPrn string, newProfile *Profile) (*Profile, error) {
	profile := &Profile{}
	account, err := a.getUserAccount(accountPrn, "prn")
	if err != nil {
		return profile, err
	}
	newProfile.Nick = account.Nick

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = col.FindOne(ctx, bson.M{"prn": accountPrn}).Decode(&profile)
	if err != nil && err == mongo.ErrNoDocuments {
		profile, err = a.MakeUserProfile(account, newProfile.UpdateableProfile)
		if err != nil {
			return profile, err
		}
	}
	if err != nil {
		return profile, err
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = col.UpdateOne(
		ctx,
		bson.M{"prn": accountPrn},
		bson.M{"$set": newProfile.UpdateableProfile},
		nil,
	)
	if err != nil {
		return nil, err
	}

	newProfile.Email = account.Email

	return newProfile, nil
}

func (a *App) updateProfileMeta(accountPrn string, metas map[string]interface{}) (*Profile, error) {
	profile := &Profile{}
	_, err := a.getUserAccount(accountPrn, "prn")
	if err != nil {
		return profile, err
	}

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = col.FindOne(ctx, bson.M{"prn": accountPrn}).Decode(&profile)
	if err != nil && err != mongo.ErrNoDocuments {
		return profile, err
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = col.UpdateOne(
		ctx,
		bson.M{"prn": accountPrn},
		bson.M{"$set": bson.M{
			"meta": metas,
		}},
		nil,
	)
	if err != nil {
		return nil, err
	}

	profile.Meta = metas
	return profile, nil
}
