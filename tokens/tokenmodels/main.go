// Copyright 2026 Pantacor Ltd.
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

	Name      string               `json:"name" bson:"name"`
	Type      accounts.AccountType `json:"type" bson:"type"`
	Prn       string               `json:"prn" bson:"prn"`
	Owner     string               `json:"owner" bson:"owner"`
	OwnerNick string               `json:"owner-nick,omitempty" bson:"owner-nick,omitempty"`
	// Secret is the composite personal access token. It is populated only on
	// the create response so the caller can record it; it is never persisted
	// (see SecretHash).
	Secret string `json:"secret,omitempty" bson:"-"`
	// SecretHash is the hex SHA-256 of Secret and is what is stored at rest.
	// A plain (fast) hash is deliberate: the secret carries 96 bits of CSPRNG
	// entropy so a slow KDF adds nothing, and the hash is verified on every
	// API request that uses the token as a Basic-auth credential.
	SecretHash  string        `json:"-" bson:"secret_hash,omitempty"`
	Scopes      []string      `json:"scopes,omitempty" bson:"scopes,omitempty"`
	ParseScopes []utils.Scope `json:"parse-scopes,omitempty" bson:"-,omitempty"`
	Deleted     bool          `json:"deleted" bson:"deleted"`
	ExpireAt    time.Time     `json:"expire-at" bson:"expire-at"`
}

func DefaultType() accounts.AccountType {
	return accounts.AccountTypeClient
}

func (token *AuthToken) ValidType() bool {
	isValid, ok := validTypes[token.Type]

	return ok && isValid
}

// HashSecret returns the hex-encoded SHA-256 digest of a personal access
// token secret, the form in which secrets are stored at rest.
func HashSecret(secret string) string {
	return utils.HashSecret(secret)
}

// SecretMatches reports whether presented is the secret this token was
// issued with, comparing digests in constant time. A token without a stored
// hash never matches.
func (token *AuthToken) SecretMatches(presented string) bool {
	if token == nil {
		return false
	}
	return utils.SecretHashMatches(token.SecretHash, presented)
}
