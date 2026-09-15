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
	"strings"
	"testing"
)

func TestRandStringUsesItsAlphabet(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fn       func(int) string
		alphabet string
	}{
		{"lower", RandStringLower, "abcdefghijklmnopqrstuvwxyz"},
		{"upper", RandStringUpper, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
		{"mixed", RandString, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"},
	} {
		got := tc.fn(64)
		if len([]rune(got)) != 64 {
			t.Errorf("%s: produced %d runes, want 64", tc.name, len([]rune(got)))
		}
		for _, r := range got {
			if !strings.ContainsRune(tc.alphabet, r) {
				t.Errorf("%s: produced %q, which is outside its alphabet", tc.name, r)
			}
		}
		if tc.fn(0) != "" || tc.fn(-1) != "" {
			t.Errorf("%s: returned something for a non-positive length", tc.name)
		}
	}
}

// These values include the PKCE device user_code, so repeats matter.
func TestRandStringDoesNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		v := RandStringUpper(8)
		if seen[v] {
			t.Fatalf("repeated %q after %d draws", v, i)
		}
		seen[v] = true
	}
}

// Indices are drawn by discarding out-of-range bytes rather than folding them
// in with a modulo. A modulo over a 26-letter alphabet would make the first
// six letters roughly 20% more likely than the rest, which this would catch.
func TestRandStringIsNotBiased(t *testing.T) {
	const perRune = 1000
	counts := map[rune]int{}
	for _, r := range RandStringLower(26 * perRune) {
		counts[r]++
	}

	if len(counts) != 26 {
		t.Fatalf("only %d of 26 letters were produced", len(counts))
	}
	for r, c := range counts {
		if c < perRune*7/10 || c > perRune*13/10 {
			t.Errorf("letter %q appeared %d times, expected about %d", r, c, perRune)
		}
	}
}
