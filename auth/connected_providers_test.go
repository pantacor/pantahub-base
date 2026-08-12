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
