// Copyright 2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package base

import (
	"encoding/json"
	"net/http"

	"gitlab.com/pantacor/pantahub-base/mqtt"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ActiveFeatures reports which optional/newer features are enabled on this
// deployment. Core hub APIs (auth, devices, trails, objects, ...) are always
// on and are intentionally not listed. Keys are stable, lowercase feature
// names the UI can key off to show or hide functionality.
func ActiveFeatures() map[string]bool {
	return map[string]bool{
		"mqtt":     mqtt.Enabled(),
		"webhooks": utils.FeatureEnabled(utils.EnvPantahubDisableWebhooks),
		"mfa":      utils.GetEnv(utils.EnvPantahubMfaEnabled) == "true",
	}
}

// featuresHandler serves the active feature map as JSON. It is unauthenticated
// (the UI needs it before login to decide what to render) and short-cacheable.
func featuresHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"features": ActiveFeatures()})
}
