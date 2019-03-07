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

var adminObjectID primitive.ObjectID
var user1ObjectID primitive.ObjectID
var user2ObjectID primitive.ObjectID
var user3ObjectID primitive.ObjectID
var examplesObjectID primitive.ObjectID
var device1ObjectID primitive.ObjectID
var device2ObjectID primitive.ObjectID
var service1ObjectID primitive.ObjectID
var service2ObjectID primitive.ObjectID
var service3ObjectID primitive.ObjectID

// SetAccountIDs : Set IDs for Demo accounts
func SetAccountIDs() {
	adminObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650001")
	user1ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650002")
	user2ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650003")
	user3ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650004")
	examplesObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650005")
	device1ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650006")
	device2ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650007")
	service1ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650008")
	service2ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650009")
	service3ObjectID, _ = primitive.ObjectIDFromHex("123651236512365123650010")
}

var DefaultAccounts = map[string]Account{
	"prn:pantahub.com:auth:/admin": Account{
		Id:           adminObjectID,
		Type:         ACCOUNT_TYPE_ADMIN,
		Prn:          "prn:pantahub.com:auth:/admin",
		Nick:         "admin",
		Email:        "no-reply-admin@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "admin",
	},
	"prn:pantahub.com:auth:/user1": Account{
		Id:           user1ObjectID,
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user1",
		Nick:         "user1",
		Email:        "no-reply-user1@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "user1",
	},
	"prn:pantahub.com:auth:/user2": Account{
		Id:           user2ObjectID,
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user2",
		Nick:         "user2",
		Email:        "no-reply-user2@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "user2",
	},
	"prn:pantahub.com:auth:/user3": Account{
		Id:           user3ObjectID,
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user3",
		Nick:         "user3",
		Email:        "no-reply-user3@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "user3",
	},
	"prn:pantahub.com:auth:/examples": Account{
		Id:           examplesObjectID,
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/examples",
		Nick:         "examples",
		Email:        "no-reply-examples@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "examples",
	},
	"prn:pantahub.com:auth:/device1": Account{
		Id:           device1ObjectID,
		Type:         ACCOUNT_TYPE_DEVICE,
		Prn:          "prn:pantahub.com:auth:/device1",
		Nick:         "device1",
		Email:        "no-reply-device1@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "device1",
	},
	"prn:pantahub.com:auth:/device2": Account{
		Id:           device2ObjectID,
		Type:         ACCOUNT_TYPE_DEVICE,
		Prn:          "prn:pantahub.com:auth:/device2",
		Nick:         "device2",
		Email:        "no-reply-device2@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "device2",
	},
	"prn:pantahub.com:auth:/service1": Account{
		Id:           service1ObjectID,
		Type:         ACCOUNT_TYPE_SERVICE,
		Prn:          "prn:pantahub.com:auth:/service1",
		Nick:         "service1",
		Email:        "no-reply-service1@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "service1",
	},
	"prn:pantahub.com:auth:/service2": Account{
		Id:           service2ObjectID,
		Type:         ACCOUNT_TYPE_SERVICE,
		Prn:          "prn:pantahub.com:auth:/service2",
		Nick:         "service2",
		Email:        "no-reply-service2@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "service2",
	},
	"prn:pantahub.com:auth:/service3": Account{
		Id:           service3ObjectID,
		Type:         ACCOUNT_TYPE_SERVICE,
		Prn:          "prn:pantahub.com:auth:/service3",
		Nick:         "service3",
		Email:        "no-reply-service3@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "service3",
	},
}
