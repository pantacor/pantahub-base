package base

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"gitlab.com/pantacor/pantahub-base/utils"
)

func TestActiveFeaturesReflectsEnv(t *testing.T) {
	defer os.Unsetenv(utils.EnvPantahubDisableWebhooks)
	defer os.Unsetenv("PANTAHUB_MQTT_ENABLED")
	defer os.Unsetenv(utils.EnvPantahubMfaEnabled)

	os.Setenv(utils.EnvPantahubDisableWebhooks, "true") // disabled
	os.Setenv("PANTAHUB_MQTT_ENABLED", "false")         // disabled
	os.Setenv(utils.EnvPantahubMfaEnabled, "true")      // enabled

	f := ActiveFeatures()
	if f["webhooks"] {
		t.Error("webhooks should be disabled")
	}
	if f["mqtt"] {
		t.Error("mqtt should be disabled")
	}
	if !f["mfa"] {
		t.Error("mfa should be enabled")
	}
}

func TestFeaturesHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	featuresHandler(rec, httptest.NewRequest(http.MethodGet, "/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"mqtt", "webhooks", "mfa"} {
		if _, ok := body.Features[k]; !ok {
			t.Errorf("missing feature key %q", k)
		}
	}

	// non-GET is rejected
	rec = httptest.NewRecorder()
	featuresHandler(rec, httptest.NewRequest(http.MethodPost, "/features", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
}
