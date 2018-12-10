//
// Copyright 2017  Pantacor Ltd.
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
package utils

import (
	crand "crypto/rand"
	"math/rand"
	"time"

	"github.com/asaskevich/govalidator"
)

func IsNick(nick string) bool {
	l := len(nick)
	if l > 3 && l < 24 {
		return true
	}
	return false
}

func IsEmail(email string) bool {
	return govalidator.IsEmail(email)
}

var r *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

func GenerateChallenge() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

	result := make([]byte, 15)
	for i := range result {
		result[i] = chars[r.Intn(len(chars))]
	}

	return string(result)
}

func GenerateSecret(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	key := make([]byte, length)

	_, err := crand.Read(key)
	if err != nil {
		return "", err
	}

	result := make([]byte, length)
	for i, v := range key {
		result[i] = chars[int(v)%len(chars)]
	}

	return string(result), nil
}
