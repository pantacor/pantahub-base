//
// Copyright 2018  Pantacor Ltd.
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
package accounts

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type AccountType string

const (
	ACCOUNT_TYPE_ADMIN   = AccountType("ADMIN")
	ACCOUNT_TYPE_DEVICE  = AccountType("DEVICE")
	ACCOUNT_TYPE_ORG     = AccountType("ORG")
	ACCOUNT_TYPE_SERVICE = AccountType("SERVICE")
	ACCOUNT_TYPE_CLIENT  = AccountType("CLIENT")
	ACCOUNT_TYPE_USER    = AccountType("USER")
)

type Account struct {
	Id primitive.ObjectID `json:"-" bson:"_id"`

	Type  AccountType `json:"type" bson:"type"`
	Email string      `json:"email" bson:"email"`
	Nick  string      `json:"nick" bson:"nick"`
	Prn   string      `json:"prn" bson:"prn"`

	Password       string `json:"password,omitempty" bson:"password"`
	PasswordBcrypt string `json:"password_bcrypt,omitempty" bson:"password_bcrypt"`
	PasswordScrypt string `json:"password_scrypt,omitempty" bson:"password_scrypt"`
	Challenge      string `json:"challenge,omitempty" bson:"challenge"`

	TimeCreated  time.Time `json:"time-created" bson:"time-created"`
	TimeModified time.Time `json:"time-modified" bson:"time-modified"`

	// Oauth2RedirectURIs is limiting the prefix available for redirecting users with oauth code and accesstoken to
	Oauth2RedirectURIs []string `json:"redirect_uris,omitempty" bson: "redirect_uris,omitempty"`
}

type AccountPublic struct {
	Id primitive.ObjectID `json:"-" bson:"_id"`

	Type  AccountType `json:"type" bson:"type"`
	Email string      `json:"email" bson:"email"`
	Nick  string      `json:"nick" bson:"nick"`
	Prn   string      `json:"prn" bson:"prn"`

	TimeCreated  time.Time `json:"time-created" bson:"time-created"`
	TimeModified time.Time `json:"time-modified" bson:"time-modified"`
}
