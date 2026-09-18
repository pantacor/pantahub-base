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

import (
	"os"
	"testing"
)

func TestFeatureEnabled(t *testing.T) {
	const k = "PANTAHUB_DISABLE_TESTFEATURE"
	defer os.Unsetenv(k)

	// unset -> enabled (default on)
	os.Unsetenv(k)
	if !FeatureEnabled(k) {
		t.Fatal("unset flag must leave the feature enabled")
	}

	for _, v := range []string{"true", "TRUE", " True ", "1", "yes", "on"} {
		os.Setenv(k, v)
		if FeatureEnabled(k) {
			t.Fatalf("%q must disable the feature", v)
		}
	}
	for _, v := range []string{"", "false", "0", "no", "off", "anything"} {
		os.Setenv(k, v)
		if !FeatureEnabled(k) {
			t.Fatalf("%q must leave the feature enabled", v)
		}
	}
}
