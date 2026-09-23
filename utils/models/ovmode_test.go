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

package models

import "testing"

func TestNeedsVerification(t *testing.T) {
	cases := []struct {
		name string
		ov   *OVModeExtension
		want bool
	}{
		{"nil", nil, false},
		{"default mode", &OVModeExtension{Mode: DefaultVerification, Status: ValidationNotNeeded}, false},
		{"claim mode pending", &OVModeExtension{Mode: ClaimVerification, Status: Pending}, false},
		{"tls pending", &OVModeExtension{Mode: TLSVerification, Status: Pending}, true},
		{"tls completed", &OVModeExtension{Mode: TLSVerification, Status: Completed}, false},
		{"manual pending", &OVModeExtension{Mode: ManualVerification, Status: Pending}, true},
		{"manual unknown status", &OVModeExtension{Mode: ManualVerification}, true},
		{"manual completed", &OVModeExtension{Mode: ManualVerification, Status: Completed}, false},
		{"manual not needed", &OVModeExtension{Mode: ManualVerification, Status: ValidationNotNeeded}, false},
	}
	for _, c := range cases {
		if got := c.ov.NeedsVerification(); got != c.want {
			t.Errorf("%s: NeedsVerification() = %v, want %v", c.name, got, c.want)
		}
	}
}
