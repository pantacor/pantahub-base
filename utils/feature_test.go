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
