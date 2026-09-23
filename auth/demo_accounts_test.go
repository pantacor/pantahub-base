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

package auth

import "testing"

func TestDemoAccountsEnabled(t *testing.T) {
	cases := map[string]bool{
		"":      false, // not configured: fail closed
		"yes":   false,
		"true":  false,
		"1":     false,
		"prod":  false,
		"false": true,
		"FALSE": true,
		" no ":  true,
		"0":     true,
		"off":   true,
	}
	for in, want := range cases {
		if got := demoAccountsEnabled(in); got != want {
			t.Errorf("demoAccountsEnabled(%q) = %v, want %v", in, got, want)
		}
	}
}
