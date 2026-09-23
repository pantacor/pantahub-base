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

package objects

import (
	"errors"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	// ObjectTokenValidSec expiration time for object token
	ObjectTokenValidSec = 86400
)

// ObjectAccessToken access token for objects
type ObjectAccessToken struct {
	// we use iss for identifying the trails endpoint
	// we use sub for identifying the requesting user wethat this claim was issued to
	// we use aud to identify the access URI in format: http://endpoint/storage-id
	// we use issued at for the time we issued this
	// we use use expires at for issuing time constrained grants
	*jwt.Token
}

// ObjectAccessClaims object claims for access
type ObjectAccessClaims struct {
	jwt.RegisteredClaims
	DispositionName string
	Size            int64
	Method          string

	// sha in hex encoding
	Sha string
}

// NewObjectAccessToken Create new Object access token
func NewObjectAccessToken(
	name string,
	method string,
	size int64,
	sha string,
	issuer string,
	subject string,
	audience string,
	issuedAt int64,
	expiresAt int64) *ObjectAccessToken {
	claims := ObjectAccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(time.Unix(issuedAt, 0)),
			ExpiresAt: jwt.NewNumericDate(time.Unix(expiresAt, 0)),
		},
		DispositionName: name,
		Size:            size,
		Method:          method,
		Sha:             sha,
	}

	o := &ObjectAccessToken{}
	o.Token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return o
}

// NewObjectAccessForSec create new access token valid for a second
func NewObjectAccessForSec(
	name string,
	method string,
	size int64,
	sha string,
	issuer string,
	subject string,
	audience string,
	validSec int64) *ObjectAccessToken {
	timeNow := time.Now().Unix()
	return NewObjectAccessToken(name, method, size, sha, issuer, subject,
		audience, timeNow, timeNow+validSec)
}

// NewFromValidToken create a object token from another valid token
func NewFromValidToken(encodedToken string) (*ObjectAccessToken, error) {
	claim := ObjectAccessClaims{}
	tok, err := jwt.ParseWithClaims(encodedToken, &claim, func(*jwt.Token) (interface{}, error) {
		return utils.GetObjectTokenSecret(), nil
	})

	if err != nil {
		return nil, err
	}

	if !tok.Valid {
		return nil, errors.New("Invalid Token")
	}

	objTok := &ObjectAccessToken{Token: tok}
	return objTok, nil
}

// Sign sign a access token
func (o *ObjectAccessToken) Sign() (string, error) {
	return o.SignedString(utils.GetObjectTokenSecret())
}

// StorageID returns the token's single audience (the storage id). ok is false
// otherwise: an empty id would make path.Join resolve to the directory.
func (c ObjectAccessClaims) StorageID() (string, bool) {
	if len(c.Audience) != 1 || c.Audience[0] == "" {
		return "", false
	}
	return c.Audience[0], true
}
