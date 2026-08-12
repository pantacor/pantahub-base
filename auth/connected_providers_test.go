package auth

import (
	"testing"
	"time"
)

func TestOAuthConnectCookieRoundTripAndTamperProtection(t *testing.T) {
	t.Setenv("PANTAHUB_JWT_SECRET", "unit-test-secret")
	now := time.Now().Truncate(time.Second)

	value, err := encodeOAuthConnectCookie("prn:::accounts:/user", now)
	if err != nil {
		t.Fatalf("encodeOAuthConnectCookie() error = %v", err)
	}
	got, err := decodeOAuthConnectCookie(value, now.Add(time.Second))
	if err != nil {
		t.Fatalf("decodeOAuthConnectCookie() error = %v", err)
	}
	if got != "prn:::accounts:/user" {
		t.Fatalf("decoded PRN = %q", got)
	}

	tampered := value[:len(value)-1] + "B"
	if _, err := decodeOAuthConnectCookie(tampered, now); err == nil {
		t.Fatal("tampered OAuth connect cookie was accepted")
	}
	if _, err := decodeOAuthConnectCookie(value, now.Add(oauthConnectCookieTTL+time.Second)); err == nil {
		t.Fatal("expired OAuth connect cookie was accepted")
	}
}

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
