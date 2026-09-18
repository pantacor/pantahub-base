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

import (
	"testing"
)

// The account PRN a connect flow binds to now travels inside the signed OAuth
// state rather than a cross-site cookie; its roundtrip/tamper/expiry coverage
// lives with the state implementation in the auth/oauth package.

func TestConnectedAccountsEnforcedDefaultsToTrue(t *testing.T) {
	t.Setenv("PANTAHUB_OAUTH_CONNECTED_ACCOUNTS_ENFORCE", "")
	if !connectedAccountsEnforced() {
		t.Fatal("connected account enforcement should be enabled by default")
	}

	t.Setenv("PANTAHUB_OAUTH_CONNECTED_ACCOUNTS_ENFORCE", "false")
	if connectedAccountsEnforced() {
		t.Fatal("connected account enforcement should be disableable explicitly")
	}
}
