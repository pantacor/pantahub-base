// Copyright 2019 Pantacor Ltd.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package utils

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"errors"

	jwtgo "github.com/dgrijalva/jwt-go"
	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

// Method define methods for encrypt supported
type Method string

const (
	// BCryptMethod method
	BCryptMethod Method = "bcrypt"
	// SCryptMethod method
	SCryptMethod Method = "scrypt"
)

type crytoMethods struct {
	BCrypt Method
	SCrypt Method
}

// CryptoMethods kind a enum for cryptography methods supported
var (
	CryptoMethods = &crytoMethods{
		BCrypt: BCryptMethod,
		SCrypt: SCryptMethod,
	}
	errMethodNotFound = errors.New("The only encrypt method supported are bcrypt and scrypt")
)

// JwtRsaKeys Public and Private keys for Jwt
type JwtRsaKeys struct {
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
}

// GetJwtRsaKeys return an JwtRsaKeys struct with public and private key
func GetJwtRsaKeys(secret, public string) (*JwtRsaKeys, error) {
	if secret == "" {
		secret = EnvPantahubJWTAuthSecret
	}

	if public == "" {
		public = EnvPantahubJWTAuthPub
	}

	jwtSecretBase64 := GetEnv(secret)
	jwtSecretPem, err := base64.StdEncoding.DecodeString(jwtSecretBase64)
	if err != nil {
		return nil, err
	}
	jwtSecret, err := jwtgo.ParseRSAPrivateKeyFromPEM(jwtSecretPem)
	if err != nil {
		return nil, err
	}

	jwtPubBase64 := GetEnv(public)
	jwtPubPem, err := base64.StdEncoding.DecodeString(jwtPubBase64)
	if err != nil {
		return nil, err
	}
	jwtPub, err := jwtgo.ParseRSAPublicKeyFromPEM(jwtPubPem)
	if err != nil {
		return nil, err
	}

	keys := &JwtRsaKeys{
		PublicKey:  jwtPub,
		PrivateKey: jwtSecret,
	}

	return keys, nil
}

// CreateJWE encrypt a JWT token
func CreateJWE(claims interface{}) (string, error) {
	keys, err := GetJwtRsaKeys(EnvPantahubJWESecret, EnvPantahubJWEPub)
	if err != nil {
		return "", err
	}

	encrypter, err := jose.NewEncrypter(
		jose.A128GCM,
		jose.Recipient{Algorithm: jose.RSA_OAEP, Key: keys.PublicKey},
		(&jose.EncrypterOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", err
	}

	raw, err := jwt.Encrypted(encrypter).Claims(claims).CompactSerialize()
	if err != nil {
		return "", err
	}

	return raw, nil
}

// ParseJWE decrypt a JWT token
func ParseJWE(raw string, out interface{}) error {
	keys, err := GetJwtRsaKeys(EnvPantahubJWESecret, EnvPantahubJWEPub)
	if err != nil {
		return err
	}

	tok, err := jwt.ParseEncrypted(raw)
	if err != nil {
		return err
	}

	err = tok.Claims(keys.PrivateKey, &out)
	if err != nil {
		return err
	}

	return nil
}

// HashPassword create a hashed version of a string
func HashPassword(password string, method Method) (string, error) {
	switch method {
	case BCryptMethod:
		return bcryptHashPassword(password)
	case SCryptMethod:
		return scryptHashPassword(password)
	default:
		return "", errMethodNotFound
	}
}

// CheckPasswordHash validate password agains a given hash
func CheckPasswordHash(password, hash string, method Method) bool {
	switch method {
	case BCryptMethod:
		return bcryptCheckPasswordHash(password, hash)
	case SCryptMethod:
		return scryptCheckPasswordHash(password, hash)
	default:
		return false
	}
}

func bcryptHashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func bcryptCheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func scryptHashPassword(password string) (string, error) {
	bytes, err := scrypt.Key([]byte(password), []byte(GetEnv(EnvPantahubScryptSecret)), 32768, 8, 1, 32)
	return hex.EncodeToString(bytes), err
}

func scryptCheckPasswordHash(password, hash string) bool {
	passwordHash, err := scryptHashPassword(password)
	return err == nil && passwordHash == hash
}
