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

package mcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const testResourceURL = "https://api.example.com/mcp"

var (
	testKey      *rsa.PrivateKey
	strangerKey  *rsa.PrivateKey
	allScope     = utils.MarshalScopes([]utils.Scope{utils.Scopes.API})[0]
	devicesScope = utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadDevices})[0]
	trailsScope  = utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadTrails})[0]
)

// TestMain installs a throwaway signing key before anything loads it:
// jwtPublicKey reads the environment once per process.
func TestMain(m *testing.M) {
	var err error
	if testKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
	if strangerKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}

	pub, err := x509.MarshalPKIXPublicKey(&testKey.PublicKey)
	if err != nil {
		panic(err)
	}
	encode := func(blockType string, der []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
	}
	os.Setenv(utils.EnvPantahubJWTAuthSecret, encode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(testKey)))
	os.Setenv(utils.EnvPantahubJWTAuthPub, encode("PUBLIC KEY", pub))
	os.Setenv(EnvMcpResourceURL, testResourceURL)

	os.Exit(m.Run())
}

// newTestService builds a service whose store is never reached: the Mongo
// client connects lazily and these tests stop at authentication, scope checks
// and tool discovery.
func newTestService(t *testing.T, allowAPITokens bool) *Service {
	t.Helper()
	t.Setenv(EnvMcpAllowAPITokens, map[bool]string{true: "true", false: "false"}[allowAPITokens])

	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	require.NoError(t, err)
	t.Cleanup(func() { client.Disconnect(context.Background()) })

	service, err := New(client, &logs.App{})
	require.NoError(t, err)
	standInForConnections(service)
	return service
}

const (
	testConnection  = "connection-that-stands"
	endedConnection = "connection-the-user-ended"
	brokenLookup    = "connection-the-database-cannot-answer-for"
)

// standInForConnections replaces the authorization server's store: these tests
// have no database behind them.
func standInForConnections(service *Service) {
	service.connections.lookup = func(_ context.Context, connectionID string) (bool, error) {
		switch connectionID {
		case testConnection:
			return true, nil
		case brokenLookup:
			return false, errors.New("database unavailable")
		}
		return false, nil
	}
}

func userClaims(scopes ...string) jwtgo.MapClaims {
	return jwtgo.MapClaims{
		"type":   "USER",
		"prn":    "prn:pantahub.com:auth:/user1",
		"nick":   "user1",
		"scopes": strings.Join(scopes, " "),
		"aud":    testResourceURL,
		"iss":    utils.GetAPIEndpoint(""),
		"cnx":    testConnection,
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
}

func sign(t *testing.T, key *rsa.PrivateKey, claims jwtgo.MapClaims) string {
	t.Helper()
	token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)
	return token
}

func rpc(t *testing.T, service *Service, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, testResourceURL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)
	return rec
}

const (
	initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
	listToolsBody  = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
)

func callBody(tool string) string {
	return `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + tool + `","arguments":{"device":"dev1"}}}`
}

func TestCanonicalResourceURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://API.Example.com/mcp/":    "https://api.example.com/mcp",
		"HTTPS://api.example.com:443/mcp": "https://api.example.com/mcp",
		"http://localhost:12365/mcp":      "http://localhost:12365/mcp",
		"http://localhost:80/mcp?x=1#y":   "http://localhost/mcp",
		"https://api.example.com":         "https://api.example.com",
	} {
		got, err := canonicalResourceURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	for _, raw := range []string{"", "/mcp", "api.example.com/mcp"} {
		_, err := canonicalResourceURL(raw)
		assert.Error(t, err, raw)
	}
}

func TestMissingTokenIsChallengedWithResourceMetadata(t *testing.T) {
	service := newTestService(t, false)

	rec := rpc(t, service, "", initializeBody)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	// The scope hint is what keeps a first connection read-only: clients take
	// the scopes to request from here before they look at the metadata, which
	// lists the write scopes too.
	assert.Equal(t,
		`Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/mcp", scope="`+
			devicesScope+" "+trailsScope+`"`,
		rec.Header().Get("WWW-Authenticate"))
}

func TestTokenRules(t *testing.T) {
	without := func(claims jwtgo.MapClaims, key string) jwtgo.MapClaims {
		delete(claims, key)
		return claims
	}
	with := func(claims jwtgo.MapClaims, key string, value interface{}) jwtgo.MapClaims {
		claims[key] = value
		return claims
	}

	cases := []struct {
		name           string
		key            *rsa.PrivateKey
		claims         jwtgo.MapClaims
		allowAPITokens bool
		want           int
	}{
		{"bound to this resource", testKey, userClaims(allScope), false, http.StatusOK},
		{"audience spelled differently", testKey, with(userClaims(allScope), "aud", "https://API.example.com:443/mcp/"), false, http.StatusOK},
		{"audience list", testKey, with(userClaims(allScope), "aud", []string{"other", testResourceURL}), false, http.StatusOK},
		{"session user", testKey, with(userClaims(allScope), "type", "SESSION"), false, http.StatusOK},
		{"api token refused by default", testKey, without(userClaims(allScope), "aud"), false, http.StatusUnauthorized},
		{"api token when allowed", testKey, without(userClaims(allScope), "aud"), true, http.StatusOK},
		{"other audience even when api tokens are allowed", testKey, with(userClaims(allScope), "aud", "prn:pantahub.com:apis:/other"), true, http.StatusUnauthorized},
		{"bound token from another issuer", testKey, with(userClaims(allScope), "iss", "https://evil.example.com"), false, http.StatusUnauthorized},
		{"connection the user ended", testKey, with(userClaims(allScope), "cnx", endedConnection), false, http.StatusUnauthorized},
		{"bound token naming no connection", testKey, without(userClaims(allScope), "cnx"), false, http.StatusUnauthorized},
		{"connection that cannot be checked fails closed", testKey, with(userClaims(allScope), "cnx", brokenLookup), false, http.StatusInternalServerError},
		{"bound token naming no issuer", testKey, without(userClaims(allScope), "iss"), false, http.StatusUnauthorized},
		{"device token", testKey, with(userClaims(allScope), "type", "DEVICE"), false, http.StatusUnauthorized},
		{"expired", testKey, with(userClaims(allScope), "exp", time.Now().Add(-time.Minute).Unix()), false, http.StatusUnauthorized},
		{"no expiry", testKey, without(userClaims(allScope), "exp"), false, http.StatusUnauthorized},
		{"no account", testKey, without(userClaims(allScope), "prn"), false, http.StatusUnauthorized},
		{"foreign key", strangerKey, userClaims(allScope), false, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newTestService(t, tc.allowAPITokens)
			rec := rpc(t, service, sign(t, tc.key, tc.claims), initializeBody)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestUnsignedTokenIsRefused(t *testing.T) {
	service := newTestService(t, true)

	token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodNone, userClaims(allScope)).
		SignedString(jwtgo.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, rpc(t, service, token, initializeBody).Code)
}

// changingTools are the tools that modify data. Everything else must be
// read-only; a tool that is neither would run without the user being asked.
var changingTools = map[string]bool{
	toolUpdateUserMeta:    true,
	toolUpdateDeviceToken: true,
	toolUpdateApp:         true,
	toolCommitRevision:    true,
	// Both put objects into the account.
	toolGetExportUploadLink: true,
	toolImportExportFromURL: true,
}

func TestToolsAreListedWithTheRightSafetyHints(t *testing.T) {
	service := newTestService(t, false)

	rec := rpc(t, service, sign(t, testKey, userClaims(devicesScope)), listToolsBody)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Title       string                 `json:"title"`
				InputSchema map[string]interface{} `json:"inputSchema"`
				Annotations struct {
					ReadOnlyHint    bool  `json:"readOnlyHint"`
					DestructiveHint *bool `json:"destructiveHint"`
				} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response), rec.Body.String())

	listed := map[string]bool{}
	for _, tool := range response.Result.Tools {
		listed[tool.Name] = true
		if changingTools[tool.Name] {
			assert.False(t, tool.Annotations.ReadOnlyHint, tool.Name)
			require.NotNil(t, tool.Annotations.DestructiveHint, tool.Name)
			assert.True(t, *tool.Annotations.DestructiveHint, "%s changes data, so the client has to ask first", tool.Name)
		} else {
			assert.True(t, tool.Annotations.ReadOnlyHint, tool.Name)
		}
		// Nothing here may create or delete: that is where secrets come from.
		for _, verb := range []string{"create", "delete", "disable", "remove_", "new_"} {
			assert.NotContains(t, tool.Name, verb, "no tool creates or deletes")
		}
		assert.NotEmpty(t, tool.Title, tool.Name)
		assert.Equal(t, "object", tool.InputSchema["type"], tool.Name)
		assert.LessOrEqual(t, len(tool.Name), 64, tool.Name)
	}
	for name := range toolScopes {
		assert.True(t, listed[name], "tool %s has scopes but is not registered", name)
	}
	assert.Len(t, response.Result.Tools, len(toolScopes), "every registered tool needs an entry in toolScopes")
}

func TestUncoveredToolCallGetsInsufficientScopeChallenge(t *testing.T) {
	service := newTestService(t, false)

	// A devices-only token calling a trails tool.
	rec := rpc(t, service, sign(t, testKey, userClaims(devicesScope)), callBody(toolListRevisions))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	assert.Contains(t, challenge, `error="insufficient_scope"`)
	assert.Contains(t, challenge, trailsScope)
	assert.Contains(t, challenge, devicesScope, "the challenge has to name every scope still needed")
	assert.Contains(t, challenge, `resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/mcp"`)
}

// The HTTP challenge is a courtesy; the tool is what guards the data. A call
// the middleware cannot read as a single request still has to be refused.
func TestToolRefusesUncoveredCallOnItsOwn(t *testing.T) {
	_, err := authorize(nil, toolListRevisions)
	assert.Error(t, err)

	caller, err := identityFrom(nil)
	assert.Nil(t, caller)
	assert.Error(t, err)

	assert.False(t, utils.MatchScope(toolScopes[toolListRevisions], []string{devicesScope}))
	assert.True(t, utils.MatchScope(toolScopes[toolListRevisions], []string{trailsScope}))
	assert.True(t, utils.MatchScope(toolScopes[toolGetDeviceLogs], []string{allScope}))
}

func TestProtectedResourceMetadata(t *testing.T) {
	service := newTestService(t, false)

	assert.Equal(t, []string{
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-protected-resource",
	}, service.MetadataPaths())

	rec := httptest.NewRecorder()
	service.MetadataHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, service.MetadataPaths()[0], nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var metadata struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metadata))
	assert.Equal(t, testResourceURL, metadata.Resource)
	assert.Len(t, metadata.AuthorizationServers, 1)
	// Every scope a token for this endpoint may carry, and never a catch-all.
	assert.ElementsMatch(t, supportedScopes(), metadata.ScopesSupported)
	assert.Contains(t, metadata.ScopesSupported, devicesScope)
	assert.NotContains(t, metadata.ScopesSupported, allScope)
}

// Reading is what a connection gets by default; each kind of change has to be
// granted by name, and a read grant never reaches a tool that changes data.
func TestChangingToolsNeedTheirOwnGrant(t *testing.T) {
	service := newTestService(t, false)
	writeDevices := utils.MarshalScopes([]utils.Scope{utils.Scopes.WriteDevices})[0]
	updateDevices := utils.MarshalScopes([]utils.Scope{utils.Scopes.UpdateDevices})[0]
	readApps := utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadApps})[0]
	writeApps := utils.MarshalScopes([]utils.Scope{utils.Scopes.WriteApps})[0]

	for tool, needed := range map[string]string{
		toolUpdateUserMeta:    writeDevices,
		toolUpdateDeviceToken: updateDevices,
		toolListApps:          readApps,
		toolGetApp:            readApps,
		toolUpdateApp:         writeApps,
	} {
		rec := rpc(t, service, sign(t, testKey, userClaims(devicesScope, trailsScope)), callBody(tool))
		require.Equal(t, http.StatusForbidden, rec.Code, tool)
		challenge := rec.Header().Get("WWW-Authenticate")
		assert.Contains(t, challenge, `error="insufficient_scope"`, tool)
		assert.Contains(t, challenge, needed, "%s: the challenge names the scope that would do", tool)
		assert.Contains(t, challenge, devicesScope, "%s: and what the connection already needs", tool)

		assert.False(t, utils.MatchScope(toolScopes[tool], defaultScopes()), "%s must not be covered by the default grant", tool)
	}

	// Being allowed to change devices is not being allowed to change apps.
	assert.False(t, utils.MatchScope(toolScopes[toolUpdateApp], []string{writeDevices, updateDevices}))
	assert.False(t, utils.MatchScope(toolScopes[toolUpdateUserMeta], []string{writeApps, readApps}))
	// Join tokens are read with the device read grant.
	assert.True(t, utils.MatchScope(toolScopes[toolListDeviceTokens], defaultScopes()))
}

func TestUserMetaPatch(t *testing.T) {
	patch, err := userMetaPatch(map[string]interface{}{"site": "lab", "net": map[string]interface{}{"mtu": 1400}}, []string{"old"})
	require.NoError(t, err)
	assert.Equal(t, "lab", patch["site"])
	assert.Contains(t, patch, "old")
	assert.Nil(t, patch["old"], "a removed key is a nil in the merge document")

	for name, tc := range map[string]struct {
		set    map[string]interface{}
		remove []string
	}{
		"nothing to do":        {nil, nil},
		"empty key":            {map[string]interface{}{" ": "x"}, nil},
		"empty key to remove":  {nil, []string{""}},
		"null hides a delete":  {map[string]interface{}{"site": nil}, nil},
		"set and remove clash": {map[string]interface{}{"site": "lab"}, []string{"site"}},
		"too large":            {map[string]interface{}{"blob": strings.Repeat("x", maxMetaBytes)}, nil},
	} {
		_, err := userMetaPatch(tc.set, tc.remove)
		assert.Error(t, err, name)
	}
}
