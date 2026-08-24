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
package utils

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// HashSecret returns the hex SHA-256 digest of a machine secret (personal
// access token, device secret, OAuth client secret): the form in which such
// secrets are stored at rest. A fast hash is deliberate - these secrets are
// CSPRNG-generated, so a slow KDF adds nothing, and they are verified on
// every request that presents them.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SecretHashMatches reports whether presented hashes to storedHash,
// comparing in constant time. Empty inputs never match.
func SecretHashMatches(storedHash, presented string) bool {
	if storedHash == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashSecret(presented)), []byte(storedHash)) == 1
}
