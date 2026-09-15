//
// Copyright 2026 Pantacor Ltd.
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

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ant0ine/go-json-rest/rest"
)

func restRequest(t *testing.T, target string, headers map[string]string) *rest.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return &rest.Request{Request: req}
}

// The previous implementation asked r.URL.Scheme, which net/http leaves empty
// on server-side requests, so Secure was never set -- not even over HTTPS.
func TestIsSecureRequestDoesNotRelyOnURLScheme(t *testing.T) {
	original, had := os.LookupEnv(EnvPantahubScheme)
	t.Cleanup(func() {
		if had {
			os.Setenv(EnvPantahubScheme, original)
		} else {
			os.Unsetenv(EnvPantahubScheme)
		}
	})

	// A request as a server actually receives it: path only, no scheme.
	os.Setenv(EnvPantahubScheme, "https")
	if !IsSecureRequest(restRequest(t, "/auth/login", nil)) {
		t.Error("an https deployment must set Secure even though r.URL.Scheme is empty")
	}
}

func TestIsSecureRequestHonoursForwardedProto(t *testing.T) {
	original, had := os.LookupEnv(EnvPantahubScheme)
	t.Cleanup(func() {
		if had {
			os.Setenv(EnvPantahubScheme, original)
		} else {
			os.Unsetenv(EnvPantahubScheme)
		}
	})

	// TLS terminates at the ingress, so the app sees plain HTTP.
	os.Setenv(EnvPantahubScheme, "http")

	if !IsSecureRequest(restRequest(t, "/auth/login", map[string]string{
		"X-Forwarded-Proto": "https",
	})) {
		t.Error("a request forwarded as https must be treated as secure")
	}

	if IsSecureRequest(restRequest(t, "/auth/login", nil)) {
		t.Error("plain http with no forwarding must not be treated as secure")
	}
}

// Local development over plain http must still get usable cookies: a Secure
// cookie would simply be dropped by the browser.
func TestIsSecureRequestFalseForPlainLocalHTTP(t *testing.T) {
	original, had := os.LookupEnv(EnvPantahubScheme)
	t.Cleanup(func() {
		if had {
			os.Setenv(EnvPantahubScheme, original)
		} else {
			os.Unsetenv(EnvPantahubScheme)
		}
	})

	os.Setenv(EnvPantahubScheme, "http")
	if IsSecureRequest(restRequest(t, "/auth/login", nil)) {
		t.Error("plain local http must not set Secure")
	}
}
