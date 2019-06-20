//Copyright 2019 Pantacor Ltd.
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
	"errors"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

// Method define methods for encrypt supported
type Method string

const (
	 // BCrypt method
	BCrypt Method = "bcrypt"
	 // SCrypt method
	SCrypt Method = "scrypt"
)

type crytoMethods struct {
	BCrypt Method
	Scrypt Method
}

CryptoMethods = &crytoMethods{
	BCrypt: BCrypt,
	Scrypt: Scrypt,
}
errMethodNotFound := errors.New("The only encrypt method supported are bcrypt and scrypt")

// HashPassword create a hashed version of a string
func HashPassword (password string, method Method) (string, error){
	switch method {
	case CryptoMethods.BCrypt:
		return bcryptHashPassword(password)
	case CryptoMethods.SCrypt:
		return scryptHashPassword(password)
	default:
		return "", errMethodNotFound
	}
}

// CheckPasswordHash validate password agains a given hash
func CheckPasswordHash (password string, method Method) (string, error){
	switch method {
	case BCrypt:
		return bcryptCheckPasswordHash(password)
	case SCrypt:
		return scryptCheckPasswordHash(password)
	default:
		return "", errMethodNotFound
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
	bytes, err := scrypt.Key([]byte(password), utils.GetEnv(utils.ENV_PANTAHUB_AUTH_SECRET), 32768, 8, 1, 32)
	return string(bytes), err
}

func scryptCheckPasswordHash(password, hash string) bool {
	passwordHash, err := ScryptHashPassword(password)
	return err == nil && string(passwordHash) == hash
}