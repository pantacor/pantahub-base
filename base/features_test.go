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
