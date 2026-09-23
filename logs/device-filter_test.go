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

package logs

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// `pvr device logs 648b56a6` passes a shortened id. It used to be treated as a
// nick, match nothing, and leave the filter empty -- and an empty device
// filter means every device the caller owns.
func TestObjectIDPrefixRangeCoversTheShortenedID(t *testing.T) {
	full, err := primitive.ObjectIDFromHex("648b56a6c2d476000dd1ba0c")
	if err != nil {
		t.Fatalf("building the reference id: %s", err)
	}

	low, high, ok := objectIDPrefixRange("648b56a6")
	if !ok {
		t.Fatal("a valid 8-character prefix was rejected")
	}

	if full.Hex() < low.Hex() || full.Hex() > high.Hex() {
		t.Errorf("id %s falls outside the range %s..%s", full.Hex(), low.Hex(), high.Hex())
	}

	if low.Hex() != "648b56a60000000000000000" {
		t.Errorf("low bound is %s", low.Hex())
	}
	if high.Hex() != "648b56a6ffffffffffffffff" {
		t.Errorf("high bound is %s", high.Hex())
	}
}

// The range must not reach into ids that merely sort nearby.
func TestObjectIDPrefixRangeExcludesOtherIDs(t *testing.T) {
	low, high, ok := objectIDPrefixRange("648b56a6")
	if !ok {
		t.Fatal("prefix rejected")
	}

	for _, other := range []string{
		"648b56a50000000000000000", // one below the prefix
		"648b56a70000000000000000", // one above
		"6a9730faeb00000000000000", // the unrelated device seen alongside it
	} {
		if other >= low.Hex() && other <= high.Hex() {
			t.Errorf("id %s should not be inside %s..%s", other, low.Hex(), high.Hex())
		}
	}
}

func TestObjectIDPrefixRangeRejectsWhatIsNotAPrefix(t *testing.T) {
	for _, in := range []string{
		"",                           // nothing
		"648b56a6c2d476000dd1ba0c",   // already a full id; the exact path handles it
		"648b56a",                    // odd length, does not land on a byte
		"my-device-nick",             // a nick
		"zzzz",                       // not hex
		"648b56a6c2d476000dd1ba0cff", // longer than an id
	} {
		if _, _, ok := objectIDPrefixRange(in); ok {
			t.Errorf("%q was accepted as an id prefix", in)
		}
	}
}

// Uppercase hex is still hex.
func TestObjectIDPrefixRangeAcceptsUppercase(t *testing.T) {
	if _, _, ok := objectIDPrefixRange("648B56A6"); !ok {
		t.Error("uppercase prefix was rejected")
	}
}

// A nick may itself be valid hex, so an id-prefix miss must fall back to a
// nick lookup rather than reporting the device as missing.
func TestObjectIDPrefixRangeTreatsHexLikeNicksAsPrefixes(t *testing.T) {
	// This is the ambiguity the fallback in ParseDeviceString exists for: the
	// string is a usable id prefix, so it is tried as one first.
	if _, _, ok := objectIDPrefixRange("abcdef"); !ok {
		t.Error("a hex-looking nick should still be tried as an id prefix first")
	}

	// Anything not hex is unambiguously a nick.
	if _, _, ok := objectIDPrefixRange("tailscale_x64_main"); ok {
		t.Error("a non-hex nick was taken for an id prefix")
	}
}

// The ids `pvr device ps` prints are 8 lowercase hex characters; that exact
// shape has to resolve, since it is what gets pasted into `pvr device logs`.
func TestObjectIDPrefixRangeAcceptsThePvrPsFormat(t *testing.T) {
	for _, id := range []string{
		"5f32dc02", "61147e8a", "623b4bd1", "648b56a6",
	} {
		if _, _, ok := objectIDPrefixRange(id); !ok {
			t.Errorf("id %q as printed by `pvr device ps` was rejected", id)
		}
	}
}
