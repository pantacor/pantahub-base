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
