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

package utils

import "testing"

func TestVerifyStoredSecret(t *testing.T) {
	h := HashSecret("s3cr3t")

	// hashed row: match by hash, never upgrade
	if ok, up := VerifyStoredSecret(h, "", "s3cr3t"); !ok || up != "" {
		t.Fatalf("hashed match: ok=%v up=%q", ok, up)
	}
	if ok, _ := VerifyStoredSecret(h, "", "wrong"); ok {
		t.Fatal("hashed row must reject a wrong secret")
	}
	// hash present alongside legacy plaintext: hash wins, no plaintext fallback
	if ok, _ := VerifyStoredSecret(h, "other-plain", "other-plain"); ok {
		t.Fatal("plaintext must be ignored once a hash exists")
	}

	// legacy plaintext-only row: match and report the hash to persist
	ok, up := VerifyStoredSecret("", "s3cr3t", "s3cr3t")
	if !ok || up != h {
		t.Fatalf("plaintext fallback: ok=%v up=%q want hash", ok, up)
	}
	if ok, _ := VerifyStoredSecret("", "s3cr3t", "wrong"); ok {
		t.Fatal("plaintext row must reject a wrong secret")
	}

	// empty inputs never match
	for _, c := range [][3]string{{"", "", "x"}, {h, "", ""}, {"", "s", ""}} {
		if ok, _ := VerifyStoredSecret(c[0], c[1], c[2]); ok {
			t.Fatalf("empty input matched: %v", c)
		}
	}
}
