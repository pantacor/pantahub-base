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

package utils

import (
	"crypto/rand"
	"math/big"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var lower = []rune("abcdefghijklmnopqrstuvwxyz")
var upper = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ")

// randString returns n runes drawn uniformly from alphabet using the
// cryptographic random source.
//
// These used to come from math/rand seeded with the process start time, which
// made every value derivable from that seed. That is fine for a device nick
// but not for the PKCE device user_code in auth/pkceservice, which is a
// credential a user types to authorise a device: predicting it lets an
// attacker complete somebody else's device authorisation. The auth code and
// session id beside it were already generated from crypto/rand, so this only
// brings the third value in line.
//
// Values outside the largest whole multiple of len(alphabet) are discarded
// rather than folded in with a modulo, which would make the low-numbered runes
// of the alphabet more likely than the rest.
func randString(alphabet []rune, n int) string {
	if n <= 0 {
		return ""
	}

	size := len(alphabet)
	limit := 256 - (256 % size)

	out := make([]rune, 0, n)
	buf := make([]byte, n)

	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			// crypto/rand does not fail on any supported platform; if the
			// system entropy source is unavailable there is no safe way to
			// carry on handing out credentials.
			panic("utils: cryptographic random source unavailable: " + err.Error())
		}

		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, alphabet[int(b)%size])
			if len(out) == n {
				break
			}
		}
	}

	return string(out)
}

// RandStringLower returns n random lowercase letters.
func RandStringLower(n int) string {
	return randString(lower, n)
}

// RandStringUpper returns n random uppercase letters.
func RandStringUpper(n int) string {
	return randString(upper, n)
}

// RandString returns n random letters, mixed case.
func RandString(n int) string {
	return randString(letters, n)
}

// RandIntn returns a uniform random int in [0, n) from the cryptographic
// random source. It panics for n <= 0, matching math/rand's Intn.
func RandIntn(n int) int {
	if n <= 0 {
		panic("utils: RandIntn requires a positive bound")
	}

	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("utils: cryptographic random source unavailable: " + err.Error())
	}

	return int(value.Int64())
}
