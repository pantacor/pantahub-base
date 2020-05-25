//
// Copyright 2020  Pantacor Ltd.
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
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var lower = []rune("abcdefghijklmnopqrstuvwxyz")
var upper = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringLower(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = lower[rand.Intn(len(lower))]
	}
	return string(b)
}

func RandStringUpper(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = upper[rand.Intn(len(upper))]
	}
	return string(b)
}

func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
