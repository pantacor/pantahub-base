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
	"time"

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func getUserByEmail(email string, db *mongo.Collection) (*accounts.Account, error) {
	newAccount := &accounts.Account{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if db == nil {
		return nil, errors.New("Error with Database connectivity")
	}

	err := db.FindOne(ctx,
		bson.M{
			"$or": []bson.M{
				{"email": email},
			},
		},
	).Decode(newAccount)

	return newAccount, err
}

func createUser(email, nick, password, challenge string, db *mongo.Collection) (*accounts.Account, error) {
	if password == "" {
		b := make([]byte, 16)
		rand.Read(b)
		password = base64.URLEncoding.EncodeToString(b)
	}

	passwordBcrypt, err := utils.HashPassword(password, utils.CryptoMethods.BCrypt)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = db.InsertOne(ctx, newAccount)

	return newAccount, err
}
