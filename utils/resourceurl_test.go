// Copyright (c) 2026 Pantacor Ltd.
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
//

package utils

import "testing"

func TestCanonicalResourceURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://API.Example.com/mcp/":    "https://api.example.com/mcp",
		"HTTPS://api.example.com:443/mcp": "https://api.example.com/mcp",
		"http://localhost:12365/mcp":      "http://localhost:12365/mcp",
		"http://localhost:80/mcp?x=1#y":   "http://localhost/mcp",
		"https://api.example.com":         "https://api.example.com",
		"https://[::1]:8443/mcp":          "https://[::1]:8443/mcp",
	} {
		got, err := CanonicalResourceURL(raw)
		if err != nil || got != want {
			t.Errorf("CanonicalResourceURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}

	for _, raw := range []string{"", "/mcp", "api.example.com/mcp", "ftp://api.example.com/mcp", "prn:pantahub.com:auth:/service1"} {
		if got, err := CanonicalResourceURL(raw); err == nil {
			t.Errorf("CanonicalResourceURL(%q) = %q, want an error", raw, got)
		}
	}
}

func TestIsResourceBoundAudience(t *testing.T) {
	for _, aud := range []interface{}{
		"https://api.example.com/mcp",
		[]string{"https://api.example.com/mcp"},
		[]interface{}{"prn:pantahub.com:auth:/service1", "http://localhost/mcp"},
	} {
		if !IsResourceBoundAudience(aud) {
			t.Errorf("IsResourceBoundAudience(%v) = false, want true", aud)
		}
	}
	for _, aud := range []interface{}{nil, "", "prn:pantahub.com:auth:/service1", []interface{}{}, 42, []interface{}{7}} {
		if IsResourceBoundAudience(aud) {
			t.Errorf("IsResourceBoundAudience(%v) = true, want false", aud)
		}
	}
}
