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
