//
// Copyright 2026 Pantacor Ltd.
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

	"github.com/asaskevich/govalidator"
)

// IsNick check if a string is a nick
func IsNick(nick string) bool {
	l := len(nick)
	if l >= 3 && l < 24 {
		return true
	}
	return false
}

// IsEmail check if a string is an email
func IsEmail(email string) bool {
	return govalidator.IsEmail(email)
}

// GenerateChallenge create challenge string
func GenerateChallenge() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

	result := make([]byte, 15)
	buf := make([]byte, 32)
	i := 0
	for i < len(result) {
		if _, err := crand.Read(buf); err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		for _, b := range buf {
			// reject bytes >= 252 (= 7*36) so the modulo below is unbiased
			if b >= 252 {
				continue
			}
			result[i] = chars[int(b)%len(chars)]
			i++
			if i == len(result) {
				break
			}
		}
	}

	return string(result)
}

// GenerateSecret generate secret
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

// CalcBinarySize calculate binary size from a string
func CalcBinarySize(data string) int {
	l := len(data)

	eq := 0
	if l >= 2 {
		if data[l-1] == '=' {
			eq++
		}
		if data[l-2] == '=' {
			eq++
		}

		l -= eq
	}

	return (l*3 - eq) / 4
}

// MergeMaps merge two maps overiding what is in the first map with the second one
func MergeMaps(base map[string]interface{}, overwrite map[string]interface{}) map[string]interface{} {
	if base == nil && overwrite != nil {
		return overwrite
	}

	if overwrite == nil && base != nil {
		return base
	}

	result := map[string]interface{}{}

	for k, v := range base {
		result[k] = v
	}

	for k, v := range overwrite {
		result[k] = v
	}

	return result
}

// MergeDefaultProjection merge projection with required values
func MergeDefaultProjection(p map[string]interface{}) map[string]interface{} {
	inclusionProjection := false
	for _, val := range p {
		if val == 1 {
			inclusionProjection = true
			break
		}
	}

	projection := map[string]interface{}{}
	if inclusionProjection {
		projection["_id"] = 1
		projection["created_at"] = 1
		projection["updated_at"] = 1
		projection["deleted_at"] = 1
		projection["owner"] = 1
	}

	for key, val := range p {
		projection[key] = val
	}

	return projection
}
