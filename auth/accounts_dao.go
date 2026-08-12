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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func getUserByEmail(ctx context.Context, email string, db *mongo.Collection) (*accounts.Account, error) {
	newAccount := &accounts.Account{}

	if db == nil {
		return nil, errors.New("error with Database connectivity")
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := db.FindOne(ctxC,
		bson.M{
			"$or": []bson.M{
				{"email": email},
			},
		},
	).Decode(newAccount)

	return newAccount, err
}

func createUser(ctx context.Context, email, nick, password, challenge string, db *mongo.Collection) (*accounts.Account, error) {
	if password == "" {
		b := make([]byte, 16)
		rand.Read(b)
		password = base64.URLEncoding.EncodeToString(b)
	}

	passwordBcrypt, err := utils.HashPassword(password, utils.CryptoMethods.BCrypt)
	if err != nil {
		return nil, err
	}

	passwordScrypt, err := utils.HashPassword(password, utils.CryptoMethods.SCrypt)
	if err != nil {
		return nil, err
	}

	mgoid := primitive.NewObjectID()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		return nil, err
	}

	createdAt := time.Now()

	newAccount := &accounts.Account{
		ID:             ObjectID,
		Prn:            "prn:::accounts:/" + ObjectID.Hex(),
		Nick:           nick,
		Email:          email,
		Password:       "",
		Challenge:      challenge,
		PasswordBcrypt: passwordBcrypt,
		PasswordScrypt: passwordScrypt,
		Type:           accounts.AccountTypeUser,
		TimeCreated:    createdAt,
		TimeModified:   createdAt,
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err = db.InsertOne(ctxC, newAccount)

	return newAccount, err
}

// getUserByPRN returns the account identified by its immutable PRN.
func getUserByPRN(ctx context.Context, prn string, db *mongo.Collection) (*accounts.Account, error) {
	if db == nil {
		return nil, errors.New("error with Database connectivity")
	}
	if prn == "" {
		return nil, errors.New("account PRN is required")
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	account := &accounts.Account{}
	err := db.FindOne(ctxC, bson.M{"prn": prn}).Decode(account)
	return account, err
}

// listConnectedProviders lists the external identities connected to an
// account. The account PRN, rather than an email address, is used as the
// ownership key because emails may change.
func listConnectedProviders(ctx context.Context, accountPRN string, db *mongo.Collection) ([]accounts.ConnectedProvider, error) {
	account, err := getUserByPRN(ctx, accountPRN, db)
	if err != nil {
		return nil, err
	}
	if account.ConnectedProviders == nil {
		return []accounts.ConnectedProvider{}, nil
	}
	return account.ConnectedProviders, nil
}

// connectProvider atomically adds an external identity to an account. The
// compound unique index on connected_providers.service/provider_id prevents a
// provider identity from being connected to two accounts.
func connectProvider(ctx context.Context, accountPRN string, provider accounts.ConnectedProvider, db *mongo.Collection) error {
	if db == nil {
		return errors.New("error with Database connectivity")
	}
	if accountPRN == "" || provider.Service == "" || provider.ProviderID == "" {
		return errors.New("account PRN, provider service and provider ID are required")
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := db.UpdateOne(
		ctxC,
		bson.M{"prn": accountPRN},
		bson.M{
			"$addToSet": bson.M{"connected_providers": provider},
			"$set":      bson.M{"time-modified": time.Now()},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// disconnectProvider removes a connected provider from an account. When
// providerID is empty, all identities for the requested service are removed;
// callers that support multiple identities for one service should provide the
// ID to remove one identity precisely.
func disconnectProvider(ctx context.Context, accountPRN, service, providerID string, db *mongo.Collection) error {
	if db == nil {
		return errors.New("error with Database connectivity")
	}
	if accountPRN == "" || service == "" {
		return errors.New("account PRN and provider service are required")
	}

	providerFilter := bson.M{"service": service}
	if providerID != "" {
		providerFilter["provider_id"] = providerID
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := db.UpdateOne(
		ctxC,
		bson.M{
			"prn":                 accountPRN,
			"connected_providers": bson.M{"$elemMatch": providerFilter},
		},
		bson.M{
			"$pull": bson.M{"connected_providers": providerFilter},
			"$set":  bson.M{"time-modified": time.Now()},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// getUserByProvider resolves an account by the provider's stable identity.
// Email is deliberately not part of this lookup: an email match is not proof
// that the external identity was previously connected to this account.
func getUserByProvider(ctx context.Context, service, providerID string, db *mongo.Collection) (*accounts.Account, error) {
	if db == nil {
		return nil, errors.New("error with Database connectivity")
	}
	if service == "" || providerID == "" {
		return nil, fmt.Errorf("provider service and provider ID are required")
	}

	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	account := &accounts.Account{}
	err := db.FindOne(ctxC, bson.M{
		"connected_providers": bson.M{
			"$elemMatch": bson.M{
				"service":     service,
				"provider_id": providerID,
			},
		},
	}).Decode(account)
	return account, err
}

// Exported DAO helpers are kept as thin wrappers so callers outside this
// package can use the same account-scoped operations without duplicating the
// Mongo filters.
func ListConnectedProviders(ctx context.Context, accountPRN string, db *mongo.Collection) ([]accounts.ConnectedProvider, error) {
	return listConnectedProviders(ctx, accountPRN, db)
}

func ConnectProvider(ctx context.Context, accountPRN string, provider accounts.ConnectedProvider, db *mongo.Collection) error {
	return connectProvider(ctx, accountPRN, provider, db)
}

func DisconnectProvider(ctx context.Context, accountPRN, service, providerID string, db *mongo.Collection) error {
	return disconnectProvider(ctx, accountPRN, service, providerID, db)
}

func GetUserByProvider(ctx context.Context, service, providerID string, db *mongo.Collection) (*accounts.Account, error) {
	return getUserByProvider(ctx, service, providerID, db)
}
