// Copyright 2024  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package tokenmodels

import (
	"time"

	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var validTypes = map[accounts.AccountType]bool{
	accounts.AccountTypeOrg:     true,
	accounts.AccountTypeService: true,
	accounts.AccountTypeClient:  true,
}

// AuthToken authentication tokens
type AuthToken struct {
	models.Timestamp `json:",inline" bson:",inline"`

	ID primitive.ObjectID `json:"id" bson:"_id"`

	Name        string               `json:"name" bson:"name"`
	Type        accounts.AccountType `json:"type" bson:"type"`
	Prn         string               `json:"prn" bson:"prn"`
	Owner       string               `json:"owner" bson:"owner"`
	OwnerNick   string               `json:"owner-nick,omitempty" bson:"owner-nick,omitempty"`
	Secret      string               `json:"secret,omitempty" bson:"secret"`
	Scopes      []string             `json:"scopes,omitempty" bson:"scopes,omitempty"`
	ParseScopes []utils.Scope        `json:"parse-scopes,omitempty" bson:"-,omitempty"`
	Deleted     bool                 `json:"deleted" bson:"deleted"`
	ExpireAt    time.Time            `json:"expire-at" bson:"expire-at"`
}

func DefaultType() accounts.AccountType {
	return accounts.AccountTypeClient
}

func (token *AuthToken) ValidType() bool {
	isValid, ok := validTypes[token.Type]

	return ok && isValid
}
