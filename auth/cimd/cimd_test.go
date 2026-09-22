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

package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudeClientID = "https://claude.ai/oauth/mcp-oauth-client-metadata"

func TestClientIDURLRules(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")

	for _, clientID := range []string{
		claudeClientID,
		"https://client.example.com/oauth/metadata.json",
		"https://client.example.com:443/app",
	} {
		_, err := validateClientIDURL(clientID)
		assert.NoError(t, err, clientID)
	}

	for clientID, why := range map[string]string{
		"prn:pantahub.com:apis:/myapp":                 "a registered application, not a url",
		"http://client.example.com/app":                "not https",
		"https://client.example.com":                   "no path",
		"https://client.example.com/":                  "no path",
		"https://user:pass@client.example.com/app":     "credentials",
		"https://client.example.com/app#frag":          "fragment",
		"https://client.example.com/a/../app":          "dot segments",
		"https://client.example.com:8443/app":          "non default port",
		"https://127.0.0.1/app":                        "ip literal",
		"https://[::1]/app":                            "ip literal",
		"https://169.254.169.254/latest/meta-data":     "ip literal",
		"https://client.example.com/" + longPath(2100): "too long",
	} {
		_, err := validateClientIDURL(clientID)
		assert.Error(t, err, why)
	}
}

func longPath(n int) string { return strings.Repeat("a", n) }

func TestAllowedHosts(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "claude.ai, Other.Example.com")

	_, err := validateClientIDURL(claudeClientID)
	assert.NoError(t, err)
	_, err = validateClientIDURL("https://other.example.com/app")
	assert.NoError(t, err)

	_, err = validateClientIDURL("https://evil.example.com/app")
	assert.ErrorIs(t, err, ErrHostNotAllowed)
	// A lookalike is a different host.
	_, err = validateClientIDURL("https://claude.ai.evil.example.com/app")
	assert.ErrorIs(t, err, ErrHostNotAllowed)
}

func TestOnlyPublicAddressesAreDialled(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "10.0.0.5", "172.16.3.4", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "224.0.0.1", "240.0.0.1", "192.0.2.10",
		"::1", "fe80::1", "fc00::1", "fd12:3456::1", "::", "64:ff9b::a00:1",
		"::ffff:10.0.0.1", "::ffff:127.0.0.1",
	} {
		assert.False(t, publicAddress(net.ParseIP(address)), address)
		assert.ErrorIs(t, guardedDial("tcp", net.JoinHostPort(address, "443"), nil), ErrBlockedAddress, address)
	}

	for _, address := range []string{"160.79.104.10", "8.8.8.8", "2606:4700::1111"} {
		assert.True(t, publicAddress(net.ParseIP(address)), address)
		assert.NoError(t, guardedDial("tcp", net.JoinHostPort(address, "443"), nil), address)
	}

	assert.False(t, publicAddress(nil), "an unparseable address is never public")
}

// The guard has to hold for a real request too, not only when called by hand.
func TestResolverRefusesToConnectInward(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the guarded client reached a loopback server")
	}))
	defer server.Close()

	resolver := NewResolver()
	_, _, err := resolver.fetch(context.Background(), server.URL+"/app")
	assert.ErrorIs(t, err, ErrBlockedAddress)
}

func document(clientID string, mutate func(map[string]interface{})) []byte {
	doc := map[string]interface{}{
		"client_id":                  clientID,
		"client_name":                "Claude",
		"client_uri":                 "https://claude.ai",
		"redirect_uris":              []string{"https://claude.ai/api/mcp/auth_callback"},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_method": "none",
	}
	if mutate != nil {
		mutate(doc)
	}
	encoded, _ := json.Marshal(doc)
	return encoded
}

// fetchFrom serves body at a test server and fetches it with that server's own
// client, which is what lets these tests reach loopback at all.
func fetchFrom(t *testing.T, handler func(clientID string, w http.ResponseWriter)) (*Document, time.Duration, error) {
	t.Helper()
	var clientID string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handler(clientID, w)
	}))
	t.Cleanup(server.Close)
	clientID = server.URL + "/oauth/client.json"

	client := server.Client()
	client.CheckRedirect = newHTTPClient().CheckRedirect
	resolver := &Resolver{client: client, cache: map[string]cacheEntry{}}
	return resolver.fetch(context.Background(), clientID)
}

func TestFetchAcceptsAWellFormedDocument(t *testing.T) {
	doc, ttl, err := fetchFrom(t, func(clientID string, w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "public, max-age=7200")
		w.Write(document(clientID, nil))
	})
	require.NoError(t, err)
	assert.Equal(t, "Claude", doc.ClientName)
	assert.Equal(t, []string{"https://claude.ai/api/mcp/auth_callback"}, doc.RedirectURIs)
	assert.Equal(t, 2*time.Hour, ttl)
}

func TestFetchRejections(t *testing.T) {
	cases := map[string]func(clientID string, w http.ResponseWriter){
		"names another client": func(_ string, w http.ResponseWriter) {
			w.Write(document(claudeClientID, nil))
		},
		"no redirect uris": func(clientID string, w http.ResponseWriter) {
			w.Write(document(clientID, func(d map[string]interface{}) { d["redirect_uris"] = []string{} }))
		},
		"plain http redirect off the user's machine": func(clientID string, w http.ResponseWriter) {
			w.Write(document(clientID, func(d map[string]interface{}) {
				d["redirect_uris"] = []string{"http://client.example.com/callback"}
			}))
		},
		"script redirect": func(clientID string, w http.ResponseWriter) {
			w.Write(document(clientID, func(d map[string]interface{}) {
				d["redirect_uris"] = []string{"javascript:alert(1)"}
			}))
		},
		"wants a client secret": func(clientID string, w http.ResponseWriter) {
			w.Write(document(clientID, func(d map[string]interface{}) {
				d["token_endpoint_auth_method"] = "client_secret_basic"
			}))
		},
		"not json": func(_ string, w http.ResponseWriter) {
			w.Write([]byte("<html>"))
		},
		"not found": func(_ string, w http.ResponseWriter) {
			w.WriteHeader(http.StatusNotFound)
		},
		"redirects elsewhere": func(_ string, w http.ResponseWriter) {
			w.Header().Set("Location", claudeClientID)
			w.WriteHeader(http.StatusFound)
		},
		"too large": func(clientID string, w http.ResponseWriter) {
			w.Write(document(clientID, func(d map[string]interface{}) {
				d["padding"] = strings.Repeat("x", maxDocument)
			}))
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := fetchFrom(t, handler)
			assert.Error(t, err)
		})
	}
}

func TestLoopbackRedirectIsAcceptedForNativeClients(t *testing.T) {
	doc, _, err := fetchFrom(t, func(clientID string, w http.ResponseWriter) {
		w.Write(document(clientID, func(d map[string]interface{}) {
			d["redirect_uris"] = []string{"http://localhost/callback", "http://127.0.0.1/callback"}
		}))
	})
	require.NoError(t, err)
	assert.Len(t, doc.RedirectURIs, 2)
}

func TestSelfAssertedFieldsAreSanitised(t *testing.T) {
	doc, _, err := fetchFrom(t, func(clientID string, w http.ResponseWriter) {
		w.Write(document(clientID, func(d map[string]interface{}) {
			d["client_name"] = "  Pantahub\x00 Official\n" + strings.Repeat("x", 200)
			d["logo_uri"] = "javascript:alert(1)"
			d["client_uri"] = "http://insecure.example.com"
		}))
	})
	require.NoError(t, err)
	assert.Len(t, []rune(doc.ClientName), 80)
	assert.NotContains(t, doc.ClientName, "\x00")
	assert.NotContains(t, doc.ClientName, "\n")
	assert.Empty(t, doc.LogoURI)
	assert.Empty(t, doc.ClientURI)
}

func TestCacheTTLIsBounded(t *testing.T) {
	assert.Equal(t, defaultCacheTTL, cacheTTL(""))
	assert.Equal(t, minCacheTTL, cacheTTL("max-age=1"))
	assert.Equal(t, maxCacheTTL, cacheTTL("max-age=99999999"))
	assert.Equal(t, 2*time.Hour, cacheTTL("public, max-age=7200"))
	assert.Equal(t, minCacheTTL, cacheTTL("no-store"))
	assert.Equal(t, defaultCacheTTL, cacheTTL("s-maxage=10"))
}

func TestFailuresAreCachedSoABadClientIDCannotBeUsedToHammer(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")
	resolver := NewResolver()
	// .invalid never resolves (RFC 6761), so this fails without leaving the host.
	clientID := "https://client.invalid/app"

	_, first := resolver.Resolve(context.Background(), clientID)
	require.Error(t, first)

	resolver.client = nil // a second fetch would panic
	_, second := resolver.Resolve(context.Background(), clientID)
	assert.Equal(t, first, second)
}

func TestCacheCannotGrowWithoutBound(t *testing.T) {
	resolver := NewResolver()
	for i := 0; i < maxCacheEntries; i++ {
		resolver.cache[string(rune(i))] = cacheEntry{expires: time.Now().Add(time.Hour)}
	}
	resolver.mu.Lock()
	resolver.evictLocked()
	resolver.mu.Unlock()
	assert.Less(t, len(resolver.cache), maxCacheEntries)
}

func TestEnabledIsOffByDefault(t *testing.T) {
	assert.False(t, Enabled())
	t.Setenv(EnvEnabled, "true")
	assert.True(t, Enabled())
	t.Setenv(EnvEnabled, "nonsense")
	assert.False(t, Enabled())
}

func TestFetchesAreThrottledPerHost(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")
	resolver := NewResolver()
	resolver.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}

	throttled := false
	for i := range 20 {
		_, err := resolver.Resolve(context.Background(), fmt.Sprintf("https://client.example.com/app-%d", i))
		if errors.Is(err, ErrThrottled) {
			throttled = true
			break
		}
	}
	assert.True(t, throttled, "a stranger cannot make this server fetch one host without bound")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
