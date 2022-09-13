//
// Copyright 2018-2020  Pantacor Ltd.
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
	"crypto/sha256"
	"encoding/hex"
	"errors"

	cjson "github.com/gibson042/canonicaljson-go"
)

func IsSha256HexString(shaString string) bool {
	sha, err := DecodeSha256HexString(shaString)

	if err == nil && len(sha) == sha256.Size {
		return true
	}
	return false
}

// DecodeSha256HexString decode sha string
func DecodeSha256HexString(shaString string) (sha []byte, err error) {
	sha, err = hex.DecodeString(shaString)

	if err == nil && len(sha) != sha256.Size {
		err = errors.New("sha does not match expected length")
	}
	return
}

// StateSha get sha state from a obj
func StateSha(obj interface{}) (string, error) {
	json, err := cjson.Marshal(obj)

	if err != nil {
		return "", err
	}

	shaHash := sha256.New()

	_, err = shaHash.Write(json)

	if err != nil {
		return "", err
	}

	arr := make([]byte, sha256.Size)
	arr = shaHash.Sum(arr[:0])
	sha := hex.EncodeToString(arr)
	return sha, err
}
