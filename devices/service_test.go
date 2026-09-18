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

package devices

import (
	"net/http/httptest"
	"testing"
)

// Only anonymous POST / and POST /register (device registration) skip auth;
// any bearer call authenticates. Paths are relative to /devices.
func TestNeedsAuth(t *testing.T) {
	for _, tc := range []struct {
		method, path, authz string
		want                bool
	}{
		{"POST", "/", "", false},
		{"POST", "/register", "", false},
		{"POST", "/", "Bearer x", true},
		{"POST", "/register", " bearer x", true},
		{"POST", "/", "Basic eDp5", false},
		{"GET", "/", "", true},
		{"PUT", "/", "", true},
		{"POST", "/register/", "", true},
		{"POST", "/tokens", "", true},
		{"POST", "/d1/ownership/validate", "", true},
	} {
		r := httptest.NewRequest(tc.method, "http://x"+tc.path, nil)
		if tc.authz != "" {
			r.Header.Set("Authorization", tc.authz)
		}
		if got := needsAuth(r); got != tc.want {
			t.Errorf("%s %s authz=%q: needsAuth=%v, want %v", tc.method, tc.path, tc.authz, got, tc.want)
		}
	}
}
