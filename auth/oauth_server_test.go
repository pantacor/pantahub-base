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

package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth/cimd"
	"gitlab.com/pantacor/pantahub-base/auth/pkceservice"
	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	oauthTestClientID = "https://client.example.com/oauth/client-metadata.json"
	oauthTestRedirect = "https://client.example.com/api/mcp/auth_callback"
	oauthTestResource = "https://api.example.com/mcp"
	oauthTestIssuer   = "https://api.example.com"
	oauthTestVerifier = "a-code-verifier-that-is-long-enough-to-be-plausible-0123456789"
)

var (
	scopeReadDevices = utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadDevices})[0]
	scopeReadTrails  = utils.MarshalScopes([]utils.Scope{utils.Scopes.ReadTrails})[0]
	scopeEverything  = utils.MarshalScopes([]utils.Scope{utils.Scopes.API})[0]
)

// metadataTransport stands in for the network: it serves the client metadata
// document of oauthTestClientID and knows no other host.
type metadataTransport struct {
	redirectURIs []string
}

func (m *metadataTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != oauthTestClientID {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	}
	body, _ := json.Marshal(map[string]interface{}{
		"client_id":                  oauthTestClientID,
		"client_name":                "Example Assistant",
		"redirect_uris":              m.redirectURIs,
		"token_endpoint_auth_method": "none",
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

type oauthFixture struct {
	t        *testing.T
	app      *App
	handler  http.Handler
	metadata *metadataTransport
	db       *mongo.Database
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	t.Setenv(utils.EnvFluentPort, "")
	t.Setenv(utils.EnvPantahubHost, "api.example.com")
	t.Setenv(utils.EnvPantahubScheme, "https")
	t.Setenv(utils.EnvPantahubPort, "")
	t.Setenv(utils.EnvPantahubWWWHost, "hub.example.com")
	t.Setenv(cimd.EnvEnabled, "true")
	t.Setenv(cimd.EnvAllowedHosts, "client.example.com")
	t.Setenv(EnvOAuthDCRAllowedRedirectHosts, "client.example.com,127.0.0.1")
	dcrPerClientLimiter = utils.NewIPRateLimiter(10.0/3600, 5)
	dcrGlobalLimiter = utils.NewIPRateLimiter(500.0/3600, 100)

	mongoClient, err := utils.GetMongoClientTest()
	require.NoError(t, err)
	db := mongoClient.Database(utils.MongoDb)
	// The PKCE and refresh token stores find their database through MONGO_DB
	// rather than through the client handed to New. Point them at the test
	// database; it must not be read again through GetMongoClientTest after
	// this, which would prefix the name a second time.
	t.Setenv(utils.EnvMongoDb, utils.MongoDb)

	app := New(&jwtauth.Config{
		Key:        []byte(contractKey),
		Realm:      contractRealm,
		Timeout:    time.Hour,
		MaxRefresh: 24 * time.Hour,
	}, mongoClient)

	metadata := &metadataTransport{redirectURIs: []string{oauthTestRedirect}}
	app.clientMetadata = cimd.NewResolverWithClient(&http.Client{Transport: metadata})
	require.NoError(t, app.RegisterOAuthResource(OAuthResource{
		URL:           oauthTestResource + "/",
		Scopes:        []string{scopeReadDevices, scopeReadTrails},
		DefaultScopes: []string{scopeReadDevices, scopeReadTrails},
	}))

	server := echoutil.NewServer("test")
	app.Mount(server)
	require.NoError(t, server.Validate())
	mux := http.NewServeMux()
	mux.Handle("/auth/", server.E)
	for _, path := range AuthorizationServerMetadataPaths() {
		mux.Handle(path, app.AuthorizationServerMetadataHandler())
	}

	return &oauthFixture{t: t, app: app, handler: mux, metadata: metadata, db: db}
}

func (f *oauthFixture) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *oauthFixture) login() string {
	return f.loginAs("user1")
}

// loginAs signs in one of the built-in development accounts, whose password is
// their nick.
func (f *oauthFixture) loginAs(nick string) string {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"`+nick+`","password":"`+nick+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "oauth-server-test")
	rec := f.do(req)
	require.Equal(f.t, http.StatusOK, rec.Code, rec.Body.String())
	var login struct{ Token string }
	require.NoError(f.t, json.Unmarshal(rec.Body.Bytes(), &login))
	require.NotEmpty(f.t, login.Token)
	return login.Token
}

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeParams(overrides map[string]string) url.Values {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {oauthTestClientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {challengeFor(oauthTestVerifier)},
		"code_challenge_method": {"S256"},
		"state":                 {"state & more=1"},
		"scope":                 {scopeReadDevices + " offline_access"},
		"resource":              {oauthTestResource},
	}
	for key, value := range overrides {
		if value == "" {
			params.Del(key)
		} else {
			params.Set(key, value)
		}
	}
	return params
}

func (f *oauthFixture) authorize(params url.Values) *httptest.ResponseRecorder {
	return f.do(httptest.NewRequest(http.MethodGet, "/auth/oauth/authorize?"+params.Encode(), nil))
}

// consent plays the web app: it reads what the API handed it on the consent
// URL and posts the user's approval to /auth/code.
func (f *oauthFixture) consent(userToken string, consentURL *url.URL) *httptest.ResponseRecorder {
	f.t.Helper()
	query := consentURL.Query()
	body, _ := json.Marshal(map[string]string{
		"service":       query.Get("client_id"),
		"scopes":        query.Get("scope"),
		"state":         query.Get("state"),
		"redirect_uri":  query.Get("redirect_uri"),
		"response_type": query.Get("response_type"),
		"auth_code":     query.Get("auth_code"),
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/code", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userToken)
	return f.do(req)
}

func (f *oauthFixture) token(form url.Values) (*httptest.ResponseRecorder, map[string]interface{}) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(req)
	response := map[string]interface{}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &response)
	return rec, response
}

// authorizedCode runs the flow up to the point where the client holds a code.
func (f *oauthFixture) authorizedCode(params url.Values) string {
	f.t.Helper()
	rec := f.authorize(params)
	require.Equal(f.t, http.StatusTemporaryRedirect, rec.Code, rec.Body.String())
	consentURL, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(f.t, err)

	approved := f.consent(f.login(), consentURL)
	require.Equal(f.t, http.StatusOK, approved.Code, approved.Body.String())
	var response codeResponse
	require.NoError(f.t, json.Unmarshal(approved.Body.Bytes(), &response))
	back, err := url.Parse(response.RedirectURI)
	require.NoError(f.t, err)
	return back.Query().Get("code")
}

func codeGrant(code string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {oauthTestVerifier},
		"redirect_uri":  {oauthTestRedirect},
		"client_id":     {oauthTestClientID},
		"resource":      {oauthTestResource},
	}
}

func claimsOf(t *testing.T, token string) jwtgo.MapClaims {
	t.Helper()
	parsed, err := jwtgo.Parse(token, func(*jwtgo.Token) (interface{}, error) { return []byte(contractKey), nil })
	require.NoError(t, err)
	return parsed.Claims.(jwtgo.MapClaims)
}

func TestAuthorizationServerMetadata(t *testing.T) {
	f := newOAuthFixture(t)

	rec := f.do(httptest.NewRequest(http.MethodGet, AuthorizationServerMetadataPath, nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var metadata authorizationServerMetadata
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metadata))
	assert.Equal(t, oauthTestIssuer, metadata.Issuer)
	assert.Equal(t, oauthTestIssuer+"/auth/oauth/authorize", metadata.AuthorizationEndpoint)
	assert.Equal(t, oauthTestIssuer+"/auth/oauth/token", metadata.TokenEndpoint)
	assert.Equal(t, oauthTestIssuer+"/auth/oauth/register", metadata.RegistrationEndpoint)
	assert.Equal(t, []string{"S256"}, metadata.CodeChallengeMethodsSupported, "clients refuse to proceed without it")
	assert.Equal(t, []string{"none"}, metadata.TokenEndpointAuthMethodsSupported)
	assert.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, metadata.GrantTypesSupported)
	assert.NotContains(t, metadata.ScopesSupported, "offline_access", "refresh tokens come without asking")
	assert.Contains(t, metadata.ScopesSupported, scopeReadDevices)
	assert.NotContains(t, metadata.ScopesSupported, scopeEverything)
	assert.True(t, metadata.ClientIDMetadataDocumentSupported)
	assert.True(t, metadata.AuthorizationResponseIssSupported)

	t.Setenv(cimd.EnvEnabled, "false")
	t.Setenv(EnvOAuthDCRAllowedRedirectHosts, "")
	rec = f.do(httptest.NewRequest(http.MethodGet, AuthorizationServerMetadataPath, nil))
	metadata = authorizationServerMetadata{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metadata))
	assert.False(t, metadata.ClientIDMetadataDocumentSupported, "not advertised unless it is switched on")
	assert.Empty(t, metadata.RegistrationEndpoint, "not advertised without hosts to register for")
}

func TestStandardClientCompletesTheWholeFlow(t *testing.T) {
	f := newOAuthFixture(t)

	// 1. The client sends the user to the authorize endpoint.
	rec := f.authorize(authorizeParams(nil))
	require.Equal(t, http.StatusTemporaryRedirect, rec.Code, rec.Body.String())
	consentURL, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "hub.example.com", consentURL.Host)
	assert.Equal(t, oauthTestClientID, consentURL.Query().Get("client_id"), "a url client id survives the hand-off intact")
	assert.Equal(t, "state & more=1", consentURL.Query().Get("state"), "a state cannot smuggle parameters in")
	assert.Equal(t, scopeReadDevices, consentURL.Query().Get("scope"))

	// 2. The user approves on the web app.
	approved := f.consent(f.login(), consentURL)
	require.Equal(t, http.StatusOK, approved.Code, approved.Body.String())
	var consentResponse codeResponse
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &consentResponse))
	back, err := url.Parse(consentResponse.RedirectURI)
	require.NoError(t, err)
	assert.Equal(t, "client.example.com", back.Host)
	assert.Equal(t, "/api/mcp/auth_callback", back.Path)
	assert.Equal(t, "state & more=1", back.Query().Get("state"))
	assert.Equal(t, oauthTestIssuer, back.Query().Get("iss"))
	code := back.Query().Get("code")
	require.NotEmpty(t, code)

	// 3. The client redeems the code, form encoded.
	rec, tokens := f.token(codeGrant(code))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
	assert.Equal(t, "Bearer", tokens["token_type"])
	assert.EqualValues(t, 3600, tokens["expires_in"])
	assert.Equal(t, scopeReadDevices, tokens["scope"])
	accessToken, _ := tokens["access_token"].(string)
	refreshToken, _ := tokens["refresh_token"].(string)
	require.NotEmpty(t, accessToken)
	require.NotEmpty(t, refreshToken)

	claims := claimsOf(t, accessToken)
	assert.Equal(t, oauthTestResource, claims["aud"])
	assert.Equal(t, oauthTestIssuer, claims["iss"])
	assert.Equal(t, oauthTestClientID, claims["client_id"])
	assert.Equal(t, "USER", claims["type"])
	assert.Equal(t, scopeReadDevices, claims["scopes"], "never the account-wide scope")
	assert.Nil(t, claims["orig_iat"], "renewed with the refresh token, not by GET /auth/login")

	// 4. The code is spent.
	rec, again := f.token(codeGrant(code))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", again["error"])

	// 5. The token is no good on the REST API.
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	assert.Equal(t, http.StatusUnauthorized, f.do(req).Code)

	// 6. The client refreshes, and gets a new refresh token with it.
	rec, refreshed := f.token(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {oauthTestClientID},
		"resource":      {oauthTestResource},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	nextRefreshToken, _ := refreshed["refresh_token"].(string)
	require.NotEmpty(t, nextRefreshToken)
	assert.NotEqual(t, refreshToken, nextRefreshToken)
	refreshedClaims := claimsOf(t, refreshed["access_token"].(string))
	assert.Equal(t, oauthTestResource, refreshedClaims["aud"])
	assert.Equal(t, scopeReadDevices, refreshedClaims["scopes"])

	// 7. The old refresh token is spent, the new one works.
	rec, stale := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {oauthTestClientID}})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", stale["error"])
	rec, _ = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {nextRefreshToken}, "client_id": {oauthTestClientID}})
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestScopesAreHeldToWhatTheResourceOffers(t *testing.T) {
	f := newOAuthFixture(t)

	// Asking for everything yields only what the resource may carry.
	code := f.authorizedCode(authorizeParams(map[string]string{"scope": scopeEverything + " " + scopeReadTrails}))
	_, tokens := f.token(codeGrant(code))
	assert.Equal(t, scopeReadTrails, tokens["scope"])

	// Asking for nothing yields the resource's defaults, not the account's.
	code = f.authorizedCode(authorizeParams(map[string]string{"scope": ""}))
	_, tokens = f.token(codeGrant(code))
	assert.Equal(t, scopeReadDevices+" "+scopeReadTrails, tokens["scope"])

	// Short names work like everywhere else.
	code = f.authorizedCode(authorizeParams(map[string]string{"scope": "devices.readonly"}))
	_, tokens = f.token(codeGrant(code))
	assert.Equal(t, scopeReadDevices, tokens["scope"])
}

func TestAuthorizeRejections(t *testing.T) {
	f := newOAuthFixture(t)

	// Before the redirect uri is trusted, errors are shown, never redirected.
	for name, overrides := range map[string]map[string]string{
		"redirect uri the client does not list": {"redirect_uri": "https://evil.example.com/callback"},
		"path beneath a listed redirect uri":    {"redirect_uri": oauthTestRedirect + "/extra"},
		"unknown url client":                    {"client_id": "https://other.example.com/client.json"},
		"unknown registered client":             {"client_id": "prn:pantahub.com:apis:/no-such-app"},
	} {
		rec := f.authorize(authorizeParams(overrides))
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Empty(t, rec.Header().Get("Location"), name)
	}

	// Once it is trusted, the client is told on its redirect uri.
	for name, tc := range map[string]struct {
		overrides map[string]string
		want      string
	}{
		"unknown resource":     {map[string]string{"resource": "https://elsewhere.example.com/mcp"}, "invalid_target"},
		"plain pkce":           {map[string]string{"code_challenge_method": "plain"}, "invalid_request"},
		"implicit grant":       {map[string]string{"response_type": "token"}, "unsupported_response_type"},
		"resource spelled odd": {map[string]string{"resource": "HTTPS://API.example.com:443/mcp/", "code_challenge_method": "plain"}, "invalid_request"},
	} {
		rec := f.authorize(authorizeParams(tc.overrides))
		require.Equal(t, http.StatusFound, rec.Code, name)
		back, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err, name)
		assert.Equal(t, "client.example.com", back.Host, name)
		assert.Equal(t, tc.want, back.Query().Get("error"), name)
		assert.Equal(t, "state & more=1", back.Query().Get("state"), name)
		assert.Equal(t, oauthTestIssuer, back.Query().Get("iss"), name)
	}
}

func TestURLClientsAreUnknownUnlessSwitchedOn(t *testing.T) {
	f := newOAuthFixture(t)
	t.Setenv(cimd.EnvEnabled, "false")

	rec := f.authorize(authorizeParams(nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestTokenRejections(t *testing.T) {
	f := newOAuthFixture(t)

	for name, mutate := range map[string]func(url.Values){
		"wrong verifier":     func(v url.Values) { v.Set("code_verifier", "not-the-verifier") },
		"wrong redirect uri": func(v url.Values) { v.Set("redirect_uri", "https://evil.example.com/cb") },
		"another client":     func(v url.Values) { v.Set("client_id", "https://other.example.com/client.json") },
		"no client":          func(v url.Values) { v.Del("client_id") },
		"another resource":   func(v url.Values) { v.Set("resource", "https://elsewhere.example.com/mcp") },
	} {
		form := codeGrant(f.authorizedCode(authorizeParams(nil)))
		mutate(form)
		rec, response := f.token(form)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Empty(t, response["access_token"], name)
	}

	// A code nobody approved is worth nothing, even with the right verifier.
	rec := f.authorize(authorizeParams(nil))
	consentURL, _ := url.Parse(rec.Header().Get("Location"))
	tokenRec, response := f.token(codeGrant(consentURL.Query().Get("auth_code")))
	assert.Equal(t, http.StatusBadRequest, tokenRec.Code)
	assert.Equal(t, "invalid_grant", response["error"])
}

func TestConsentCannotBeRedirectedOrTakenOver(t *testing.T) {
	f := newOAuthFixture(t)
	userToken := f.login()

	rec := f.authorize(authorizeParams(nil))
	consentURL, _ := url.Parse(rec.Header().Get("Location"))

	// The browser lies about where to go and which state to echo: what was
	// stored when the client started the flow wins.
	tampered := *consentURL
	query := tampered.Query()
	query.Set("redirect_uri", "https://evil.example.com/steal")
	query.Set("state", "attacker-state")
	tampered.RawQuery = query.Encode()
	approved := f.consent(userToken, &tampered)
	require.Equal(t, http.StatusOK, approved.Code, approved.Body.String())
	var response codeResponse
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &response))
	back, _ := url.Parse(response.RedirectURI)
	assert.Equal(t, "client.example.com", back.Host)
	assert.Equal(t, "state & more=1", back.Query().Get("state"))

	// Naming another client for the same authorization is refused.
	rec = f.authorize(authorizeParams(nil))
	consentURL, _ = url.Parse(rec.Header().Get("Location"))
	mismatched := *consentURL
	query = mismatched.Query()
	query.Set("client_id", "https://other.example.com/client.json")
	mismatched.RawQuery = query.Encode()
	assert.Equal(t, http.StatusBadRequest, f.consent(userToken, &mismatched).Code)

	// A client that dropped the redirect uri from its document since the flow
	// started no longer gets a code delivered there.
	rec = f.authorize(authorizeParams(nil))
	consentURL, _ = url.Parse(rec.Header().Get("Location"))
	f.metadata.redirectURIs = []string{"https://client.example.com/somewhere/else"}
	f.app.clientMetadata = cimd.NewResolverWithClient(&http.Client{Transport: f.metadata})
	assert.Equal(t, http.StatusBadRequest, f.consent(userToken, consentURL).Code)
}

func TestRefreshTokenIsBoundToItsClient(t *testing.T) {
	f := newOAuthFixture(t)
	_, tokens := f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
	refreshToken := tokens["refresh_token"].(string)

	rec, response := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {"https://other.example.com/client.json"}})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", response["error"])

	// The rightful client was not locked out by somebody else's attempt.
	rec, _ = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {oauthTestClientID}})
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// A refresh may narrow the scopes but never widen them.
	_, tokens = f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
	rec, response = f.token(url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)},
		"client_id": {oauthTestClientID}, "scope": {scopeReadTrails},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, "trails was never granted")
	assert.Equal(t, "invalid_scope", response["error"])
}

// A refresh token presented again long after it was used means two parties
// hold it. Both are signed out.
func TestReplayedRefreshTokenRevokesTheFamily(t *testing.T) {
	f := newOAuthFixture(t)
	_, tokens := f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
	first := tokens["refresh_token"].(string)

	_, refreshed := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}, "client_id": {oauthTestClientID}})
	second := refreshed["refresh_token"].(string)
	require.NotEmpty(t, second)

	// Age the first use past the retry grace.
	collection := f.db.Collection("pantahub_" + storage.RefreshTokenCollection)
	result, err := collection.UpdateOne(context.Background(),
		bson.M{"token_hash": utils.HashSecret(first)},
		bson.M{"$set": bson.M{"used_at": time.Now().Add(-time.Hour)}})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.MatchedCount, "the refresh token is stored hashed, under its collection")

	rec, _ := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}, "client_id": {oauthTestClientID}})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec, response := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {second}, "client_id": {oauthTestClientID}})
	assert.Equal(t, http.StatusBadRequest, rec.Code, "the whole family is revoked")
	assert.Equal(t, "invalid_grant", response["error"])
}

func TestRefreshTokenIsNeverStoredInTheClear(t *testing.T) {
	f := newOAuthFixture(t)
	_, tokens := f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
	refreshToken := tokens["refresh_token"].(string)

	collection := f.db.Collection("pantahub_" + storage.RefreshTokenCollection)
	cursor, err := collection.Find(context.Background(), bson.M{})
	require.NoError(t, err)
	var stored []bson.M
	require.NoError(t, cursor.All(context.Background(), &stored))
	require.NotEmpty(t, stored)
	encoded, _ := json.Marshal(stored)
	assert.NotContains(t, string(encoded), refreshToken)
}

// The clients written against this endpoint before send JSON, name no resource
// and read "token". Nothing about what they get may change.
func TestOlderClientsGetWhatTheyAlwaysGot(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()

	pks, err := pkceservice.CreatePKCEState(ctx, challengeFor(oauthTestVerifier), "S256",
		"http://localhost:8787/callback", "s", "prn:pantahub.com:apis:/some-cli", "")
	require.NoError(t, err)
	require.True(t, pkceservice.UpdatePKCEStateUserID(ctx, pks.AuthCode, "prn:pantahub.com:auth:/user1"))

	body, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"access-code":   pks.AuthCode,
		"code_verifier": oauthTestVerifier,
		"redirect_uri":  "http://localhost:8787/callback",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/token", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := f.do(req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	response := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	token, _ := response["token"].(string)
	require.NotEmpty(t, token, "the field older clients read")
	assert.Equal(t, token, response["access_token"], "the same token under its standard name")
	assert.Equal(t, "bearer", response["token_type"])
	assert.Nil(t, response["refresh_token"], "they never asked for one")

	claims := claimsOf(t, token)
	assert.Nil(t, claims["aud"], "an unbound token, as before")
	assert.Nil(t, claims["iss"])
	assert.Equal(t, scopeEverything, claims["scopes"], "the account default, as before")
	assert.NotNil(t, claims["orig_iat"], "still renewable through GET /auth/login")

	// And it still works on the REST API.
	refresh := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	refresh.Header.Set("Authorization", "Bearer "+token)
	assert.Equal(t, http.StatusOK, f.do(refresh).Code)

	// The code is spent for them too.
	again := httptest.NewRequest(http.MethodPost, "/auth/oauth/token", strings.NewReader(string(body)))
	again.Header.Set("Content-Type", "application/json")
	assert.Equal(t, http.StatusBadRequest, f.do(again).Code)
}

func TestOAuthClientInfoForTheConsentPage(t *testing.T) {
	f := newOAuthFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/client?client_id="+url.QueryEscape(oauthTestClientID), nil)
	assert.Equal(t, http.StatusUnauthorized, f.do(req).Code, "not an anonymous url fetcher")

	req = httptest.NewRequest(http.MethodGet, "/auth/oauth/client?client_id="+url.QueryEscape(oauthTestClientID), nil)
	req.Header.Set("Authorization", "Bearer "+f.login())
	rec := f.do(req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var info oauthClientInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &info))
	assert.Equal(t, "Example Assistant", info.Name)
	assert.Equal(t, "client.example.com", info.Host)
	assert.Equal(t, []string{oauthTestRedirect}, info.RedirectURIs)
}

func (f *oauthFixture) connections(userToken string) (*httptest.ResponseRecorder, []oauthConnection) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/connections", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	rec := f.do(req)
	var listed []oauthConnection
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	return rec, listed
}

func (f *oauthFixture) disconnect(userToken, id string) int {
	req := httptest.NewRequest(http.MethodDelete, "/auth/oauth/connections/"+url.PathEscape(id), nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	return f.do(req).Code
}

// forget drops the connections earlier tests left behind for this account.
func (f *oauthFixture) forget() {
	_, err := f.db.Collection("pantahub_"+storage.RefreshTokenCollection).DeleteMany(context.Background(), bson.M{})
	require.NoError(f.t, err)
}

func TestUserSeesAndEndsTheirConnections(t *testing.T) {
	f := newOAuthFixture(t)
	f.forget()
	user := f.login()

	rec, listed := f.connections(user)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()), "an empty list, not null")
	assert.Empty(t, listed)

	_, tokens := f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
	accessToken := tokens["access_token"].(string)
	refreshToken := tokens["refresh_token"].(string)
	connectionID, _ := claimsOf(t, accessToken)[ClaimConnectionID].(string)
	require.NotEmpty(t, connectionID, "the access token names its connection")

	rec, listed = f.connections(user)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, listed, 1)
	connection := listed[0]
	assert.Equal(t, connectionID, connection.ID)
	assert.Equal(t, oauthTestClientID, connection.ClientID)
	assert.Equal(t, "Example Assistant", connection.ClientName)
	assert.Equal(t, "client.example.com", connection.ClientHost)
	assert.Equal(t, oauthTestResource, connection.Resource)
	assert.Equal(t, []string{scopeReadDevices}, connection.Scopes)
	assert.WithinDuration(t, time.Now(), connection.GrantedAt, time.Minute)
	assert.NotContains(t, rec.Body.String(), refreshToken)
	assert.NotContains(t, rec.Body.String(), "token_hash")

	// Refreshing keeps it one connection, granted when it was first granted.
	_, refreshed := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {oauthTestClientID}})
	refreshToken = refreshed["refresh_token"].(string)
	assert.Equal(t, connectionID, claimsOf(t, refreshed["access_token"].(string))[ClaimConnectionID])
	_, listed = f.connections(user)
	require.Len(t, listed, 1)
	assert.Equal(t, connectionID, listed[0].ID)
	assert.True(t, listed[0].GrantedAt.Equal(connection.GrantedAt), "granted_at survives rotation")
	assert.Equal(t, "Example Assistant", listed[0].ClientName)

	repo, err := storage.GetRefreshTokenRepo()
	require.NoError(t, err)
	active, err := repo.FamilyActive(context.Background(), connectionID)
	require.NoError(t, err)
	assert.True(t, active)

	// The user disconnects it.
	assert.Equal(t, http.StatusNoContent, f.disconnect(user, connectionID))

	_, listed = f.connections(user)
	assert.Empty(t, listed)
	active, err = repo.FamilyActive(context.Background(), connectionID)
	require.NoError(t, err)
	assert.False(t, active, "what the resource asks before accepting an access token issued under it")
	rec, response := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {oauthTestClientID}})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", response["error"], "so the client asks the user to connect again")

	assert.Equal(t, http.StatusNotFound, f.disconnect(user, connectionID), "already ended")
}

func TestConnectionsBelongToTheirUser(t *testing.T) {
	f := newOAuthFixture(t)
	f.forget()
	owner := f.login()
	other := f.loginAs("user2")

	_, tokens := f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
	accessToken := tokens["access_token"].(string)
	connectionID := claimsOf(t, accessToken)[ClaimConnectionID].(string)

	_, listed := f.connections(other)
	assert.Empty(t, listed, "nobody sees somebody else's connections")
	assert.Equal(t, http.StatusNotFound, f.disconnect(other, connectionID), "or ends them, even knowing the id")

	_, listed = f.connections(owner)
	require.Len(t, listed, 1, "still connected")

	// A connected application holds a resource-bound token, which this API
	// refuses: it cannot list the user's connections or end one, its own
	// included.
	rec, _ := f.connections(accessToken)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, http.StatusUnauthorized, f.disconnect(accessToken, connectionID))

	rec, _ = f.connections("")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// A password reset ends every connection of the account (handlePasswordReset),
// through this.
func TestRevokeUserEndsEveryConnectionOfThatAccountOnly(t *testing.T) {
	f := newOAuthFixture(t)
	f.forget()
	user := f.login()

	var connectionIDs []string
	for i := 0; i < 2; i++ {
		_, tokens := f.token(codeGrant(f.authorizedCode(authorizeParams(nil))))
		connectionIDs = append(connectionIDs, claimsOf(t, tokens["access_token"].(string))[ClaimConnectionID].(string))
	}
	_, listed := f.connections(user)
	require.Len(t, listed, 2)

	repo, err := storage.GetRefreshTokenRepo()
	require.NoError(t, err)
	require.NoError(t, repo.RevokeUser(context.Background(), "prn:pantahub.com:auth:/user2"))
	_, listed = f.connections(user)
	assert.Len(t, listed, 2, "somebody else's reset does not touch these")

	require.NoError(t, repo.RevokeUser(context.Background(), "prn:pantahub.com:auth:/user1"))
	_, listed = f.connections(user)
	assert.Empty(t, listed)
	for _, id := range connectionIDs {
		active, err := repo.FamilyActive(context.Background(), id)
		require.NoError(t, err)
		assert.False(t, active)
	}
}

func TestDynamicClientRegistration_Valid(t *testing.T) {
	f := newOAuthFixture(t)

	body := `{
		"client_name": "opencode",
		"redirect_uris": ["http://127.0.0.1:19876/mcp/oauth/callback"],
		"token_endpoint_auth_method": "none",
		"grant_types": ["authorization_code", "refresh_token"],
		"response_types": ["code"]
	}`

	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := f.do(req)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
	assert.Equal(t, "no-cache", rec.Header().Get("Pragma"))

	var res ClientRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))

	assert.True(t, strings.HasPrefix(res.ClientID, "prn:::apps:/"), "client_id should be a PRN: %s", res.ClientID)
	assert.True(t, res.ClientIDIssuedAt > 0)
	assert.Equal(t, "opencode", res.ClientName)
	assert.Equal(t, []string{"http://127.0.0.1:19876/mcp/oauth/callback"}, res.RedirectURIs)
	assert.Equal(t, "none", res.TokenEndpointAuthMethod)
	assert.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, res.GrantTypes)
	assert.Equal(t, []string{"code"}, res.ResponseTypes)
}

func TestDynamicClientRegistration_Validation(t *testing.T) {
	f := newOAuthFixture(t)

	cases := []struct {
		name        string
		body        string
		expectedErr string
	}{
		{
			name:        "empty redirect_uris",
			body:        `{"client_name":"test","redirect_uris":[]}`,
			expectedErr: "invalid_redirect_uri",
		},
		{
			name:        "invalid redirect_uri",
			body:        `{"client_name":"test","redirect_uris":["not-a-valid-uri"]}`,
			expectedErr: "invalid_redirect_uri",
		},
		{
			name:        "remote cleartext http",
			body:        `{"client_name":"test","redirect_uris":["http://example.com/callback"]}`,
			expectedErr: "invalid_redirect_uri",
		},
		{
			name:        "unsupported auth method",
			body:        `{"client_name":"test","redirect_uris":["http://127.0.0.1:19876/callback"],"token_endpoint_auth_method":"client_secret_basic"}`,
			expectedErr: "invalid_client_metadata",
		},
		{
			name:        "unsupported grant type",
			body:        `{"client_name":"test","redirect_uris":["http://127.0.0.1:19876/callback"],"grant_types":["client_credentials"]}`,
			expectedErr: "invalid_client_metadata",
		},
		{
			name:        "unsupported response type",
			body:        `{"client_name":"test","redirect_uris":["http://127.0.0.1:19876/callback"],"response_types":["token"]}`,
			expectedErr: "invalid_client_metadata",
		},
		{
			name:        "redirect host not on the allow list",
			body:        `{"client_name":"test","redirect_uris":["https://evil.example.com/callback"]}`,
			expectedErr: "invalid_redirect_uri",
		},
		{
			name:        "one of several redirect hosts not on the allow list",
			body:        `{"client_name":"test","redirect_uris":["https://client.example.com/cb","https://evil.example.com/cb"]}`,
			expectedErr: "invalid_redirect_uri",
		},
		{
			name:        "malformed json",
			body:        `{bad-json`,
			expectedErr: "invalid_client_metadata",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := f.do(req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			var errRes map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errRes))
			assert.Equal(t, tc.expectedErr, errRes["error"])
			assert.NotEmpty(t, errRes["error_description"])
		})
	}

	t.Run("disabled without an allow list", func(t *testing.T) {
		t.Setenv(EnvOAuthDCRAllowedRedirectHosts, "")
		req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(`{"redirect_uris":["http://127.0.0.1:19876/callback"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := f.do(req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestDynamicClientRegistration_EndToEndFlow(t *testing.T) {
	f := newOAuthFixture(t)
	f.forget()
	user := f.login()

	// 1. Dynamic client registers
	regBody := `{
		"client_name": "opencode",
		"redirect_uris": ["http://127.0.0.1:19876/mcp/oauth/callback"],
		"token_endpoint_auth_method": "none"
	}`
	regReq := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := f.do(regReq)
	require.Equal(t, http.StatusCreated, regRec.Code, regRec.Body.String())

	var regRes ClientRegistrationResponse
	require.NoError(t, json.Unmarshal(regRec.Body.Bytes(), &regRes))
	clientID := regRes.ClientID
	redirectURI := "http://127.0.0.1:19876/mcp/oauth/callback"

	// 2. Consent page fetches client metadata
	clientReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/client?client_id="+url.QueryEscape(clientID), nil)
	clientReq.Header.Set("Authorization", "Bearer "+user)
	clientRec := f.do(clientReq)
	require.Equal(t, http.StatusOK, clientRec.Code, clientRec.Body.String())
	var info oauthClientInfo
	require.NoError(t, json.Unmarshal(clientRec.Body.Bytes(), &info))
	assert.Equal(t, clientID, info.ClientID)
	assert.Equal(t, "opencode", info.Name)
	assert.Equal(t, []string{redirectURI}, info.RedirectURIs)

	// 3. Client starts authorization with PKCE and resource
	verifier := "opencode-code-verifier-long-enough-0123456789"
	authParams := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challengeFor(verifier)},
		"code_challenge_method": {"S256"},
		"state":                 {"opencode-state-123"},
		"scope":                 {scopeReadDevices + " offline_access"},
		"resource":              {oauthTestResource},
	}
	authRec := f.authorize(authParams)
	require.Equal(t, http.StatusTemporaryRedirect, authRec.Code)
	consentURL, err := url.Parse(authRec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, clientID, consentURL.Query().Get("client_id"))

	// 4. User grants consent
	consentRec := f.consent(user, consentURL)
	require.Equal(t, http.StatusOK, consentRec.Code, consentRec.Body.String())
	var consentRes codeResponse
	require.NoError(t, json.Unmarshal(consentRec.Body.Bytes(), &consentRes))
	redirectTarget, err := url.Parse(consentRes.RedirectURI)
	require.NoError(t, err)
	code := redirectTarget.Query().Get("code")
	require.NotEmpty(t, code)
	assert.Equal(t, "opencode-state-123", redirectTarget.Query().Get("state"))
	assert.Equal(t, oauthTestIssuer, redirectTarget.Query().Get("iss"), "standard clients must receive iss")

	// 5. Client exchanges authorization code for tokens
	tokenRec, tokens := f.token(codeGrantWithClient(code, clientID, redirectURI, verifier, oauthTestResource))
	require.Equal(t, http.StatusOK, tokenRec.Code, tokenRec.Body.String())
	accessToken, ok := tokens["access_token"].(string)
	require.True(t, ok)
	refreshToken, ok := tokens["refresh_token"].(string)
	require.True(t, ok)

	claims := claimsOf(t, accessToken)
	assert.Equal(t, oauthTestResource, claims["aud"])
	assert.Equal(t, oauthTestIssuer, claims["iss"])
	assert.Equal(t, clientID, claims["client_id"])
	connectionID := claims[ClaimConnectionID].(string)
	assert.NotEmpty(t, connectionID)

	// 6. Refresh token rotation
	refreshRec, refreshed := f.token(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	})
	require.Equal(t, http.StatusOK, refreshRec.Code, refreshRec.Body.String())
	require.NotEmpty(t, refreshed["access_token"])
	require.NotEmpty(t, refreshed["refresh_token"])
	assert.NotEqual(t, refreshToken, refreshed["refresh_token"])

	// 7. Verify connection is listed for user
	_, connections := f.connections(user)
	require.Len(t, connections, 1)
	assert.Equal(t, connectionID, connections[0].ID)
	assert.Equal(t, "opencode", connections[0].ClientName)
	assert.Equal(t, clientID, connections[0].ClientID)
}

func codeGrantWithClient(code, clientID, redirectURI, verifier, resource string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"client_id":     {clientID},
		"resource":      {resource},
	}
}

// register plays a client registering itself and returns its client_id.
func (f *oauthFixture) register(redirectURIs ...string) string {
	f.t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"client_name": "self registered", "redirect_uris": redirectURIs})
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := f.do(req)
	require.Equal(f.t, http.StatusCreated, rec.Code, rec.Body.String())
	var res ClientRegistrationResponse
	require.NoError(f.t, json.Unmarshal(rec.Body.Bytes(), &res))
	return res.ClientID
}

// The consent URL passes through the browser of whoever started the flow, so
// the handle on it must never be redeemable: otherwise an attacker starts a
// flow for a trusted client, has the victim approve it, and redeems the code
// with their own verifier.
func TestConsentHandleIsNeverACode(t *testing.T) {
	f := newOAuthFixture(t)

	rec := f.authorize(authorizeParams(nil))
	require.Equal(t, http.StatusTemporaryRedirect, rec.Code, rec.Body.String())
	consentURL, _ := url.Parse(rec.Header().Get("Location"))
	handle := consentURL.Query().Get("auth_code")
	require.NotEmpty(t, handle)

	rec, _ = f.token(codeGrant(handle))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "not before consent")

	approved := f.consent(f.login(), consentURL)
	require.Equal(t, http.StatusOK, approved.Code, approved.Body.String())
	var response codeResponse
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &response))
	back, _ := url.Parse(response.RedirectURI)
	code := back.Query().Get("code")
	assert.NotEqual(t, handle, code, "the code is minted at consent")
	assert.Equal(t, code, response.Code)

	rec, _ = f.token(codeGrant(handle))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "not after consent either")

	// Approving the same handle again gets nothing.
	assert.Equal(t, http.StatusBadRequest, f.consent(f.login(), consentURL).Code)

	rec, _ = f.token(codeGrant(code))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestUntrustedClientsOnlyGetBoundTokens(t *testing.T) {
	f := newOAuthFixture(t)
	dynamic := f.register("http://127.0.0.1:19876/callback")

	authorizeError := func(params url.Values) string {
		t.Helper()
		rec := f.authorize(params)
		require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
		target, _ := url.Parse(rec.Header().Get("Location"))
		return target.Query().Get("error")
	}

	// Without a resource a URL client or a self-registered one would get an
	// account-wide token; both are told to name one instead.
	assert.Equal(t, "invalid_target", authorizeError(authorizeParams(map[string]string{"resource": ""})))
	assert.Equal(t, "invalid_target", authorizeError(authorizeParams(map[string]string{
		"resource":     "",
		"client_id":    dynamic,
		"redirect_uri": "http://127.0.0.1:19876/callback",
	})))

	// The polling flow mints account-wide tokens, so it is closed to them.
	for _, clientID := range []string{dynamic, oauthTestClientID} {
		body, _ := json.Marshal(map[string]string{
			"client_id":             clientID,
			"redirect_uri":          "http://127.0.0.1:19876/callback",
			"code_challenge":        challengeFor(oauthTestVerifier),
			"code_challenge_method": "S256",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/oauth/pkce/init", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		assert.Equal(t, http.StatusBadRequest, f.do(req).Code, clientID)
	}

	// Nor can the older consent flows be pointed at a self-registered client.
	body, _ := json.Marshal(map[string]string{
		"service":       dynamic,
		"redirect_uri":  "http://127.0.0.1:19876/callback",
		"response_type": "token",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/code", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.login())
	assert.Equal(t, http.StatusBadRequest, f.do(req).Code)

	// And a state that somehow holds no resource is refused at the token
	// endpoint all the same.
	ctx := context.Background()
	pks, err := pkceservice.CreatePKCEState(ctx, challengeFor(oauthTestVerifier), "S256",
		"http://127.0.0.1:19876/callback", "s", dynamic, "")
	require.NoError(t, err)
	approved, ok := pkceservice.ApprovePKCEState(ctx, pks.AuthCode, "prn:pantahub.com:auth:/user1")
	require.True(t, ok)
	rec, _ := f.token(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {approved.AuthCode},
		"code_verifier": {oauthTestVerifier},
		"redirect_uri":  {"http://127.0.0.1:19876/callback"},
		"client_id":     {dynamic},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestDynamicClientsAreHeldToExactlyWhatTheyRegistered(t *testing.T) {
	f := newOAuthFixture(t)
	dynamic := f.register("https://client.example.com/cb")

	var stored struct {
		Dynamic bool          `bson:"dynamic"`
		Scopes  []utils.Scope `bson:"scopes"`
		Owner   string        `bson:"owner"`
	}
	require.NoError(t, f.db.Collection("pantahub_apps").FindOne(context.Background(), bson.M{"prn": dynamic}).Decode(&stored))
	assert.True(t, stored.Dynamic)
	assert.Empty(t, stored.Scopes, "no account-wide scopes of its own")
	assert.Empty(t, stored.Owner)

	params := authorizeParams(map[string]string{"client_id": dynamic, "redirect_uri": "https://client.example.com/cb/deeper"})
	rec := f.authorize(params)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "no paths beneath a registered one")

	params.Set("redirect_uri", "https://client.example.com/cb")
	assert.Equal(t, http.StatusTemporaryRedirect, f.authorize(params).Code)
}

func TestDynamicRegistrationIsThrottled(t *testing.T) {
	f := newOAuthFixture(t)

	body := `{"redirect_uris":["http://127.0.0.1:19876/callback"]}`
	codes := []int{}
	for range 7 {
		req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		codes = append(codes, f.do(req).Code)
	}
	assert.Equal(t, []int{201, 201, 201, 201, 201, 429, 429}, codes)
}

func TestDynamicRegistrationSanitisesWhatItShows(t *testing.T) {
	f := newOAuthFixture(t)

	body := `{"client_name":"Pantahub\u0000\u001b[2J Official","logo_uri":"javascript:alert(1)","client_uri":"http://plain.example.com","redirect_uris":["http://127.0.0.1:19876/callback"]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := f.do(req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var res ClientRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, "Pantahub[2J Official", res.ClientName)
	assert.Empty(t, res.LogoURI)
	assert.Empty(t, res.ClientURI)
}

func TestConsentPageShowsTheHostOfTheRedirectInUse(t *testing.T) {
	f := newOAuthFixture(t)
	dynamic := f.register("http://127.0.0.1:19876/callback", "https://client.example.com/cb")
	user := f.login()

	info := func(query string) (int, oauthClientInfo) {
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/client?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+user)
		rec := f.do(req)
		var got oauthClientInfo
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		return rec.Code, got
	}

	code, got := info("client_id=" + url.QueryEscape(dynamic) + "&redirect_uri=" + url.QueryEscape("https://client.example.com/cb"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "client.example.com", got.Host, "not the first registered one")
	assert.True(t, got.Unverified)

	code, _ = info("client_id=" + url.QueryEscape(dynamic) + "&redirect_uri=" + url.QueryEscape("https://evil.example.com/cb"))
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestDynamicRegistrationListIsSeparateFromURLClientHosts(t *testing.T) {
	f := newOAuthFixture(t)

	// A host URL client ids may live on is not thereby a host a
	// self-registered client may redirect to.
	t.Setenv(cimd.EnvAllowedHosts, "client.example.com,other.example.com")
	body := `{"redirect_uris":["https://other.example.com/cb"]}`
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	assert.Equal(t, http.StatusBadRequest, f.do(req).Code)

	// And an empty URL client list (any public host) does not open
	// registration to any host.
	t.Setenv(cimd.EnvAllowedHosts, "")
	req = httptest.NewRequest(http.MethodPost, "/auth/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	assert.Equal(t, http.StatusBadRequest, f.do(req).Code)
}

func TestLegacyDynamicClientsAreMarked(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()
	collection := f.db.Collection("pantahub_apps")

	legacyID, ownedID := primitive.NewObjectID(), primitive.NewObjectID()
	_, err := collection.InsertMany(ctx, []interface{}{
		// What the first version of registration stored.
		bson.M{"_id": legacyID, "prn": "prn:::apps:/" + legacyID.Hex(), "nick": "opencode-" + legacyID.Hex(),
			"owner": "", "type": "pkce", "scopes": utils.PhScopeArray},
		// An application somebody registered through /apps.
		bson.M{"_id": ownedID, "prn": "prn:::apps:/" + ownedID.Hex(), "nick": "mine-" + ownedID.Hex(),
			"owner": "prn:pantahub.com:auth:/user1", "type": "pkce", "scopes": utils.PhScopeArray},
	})
	require.NoError(t, err)

	marked, err := markLegacyDynamicClients(ctx, f.db)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, marked, int64(1))

	var legacy, owned bson.M
	require.NoError(t, collection.FindOne(ctx, bson.M{"_id": legacyID}).Decode(&legacy))
	require.NoError(t, collection.FindOne(ctx, bson.M{"_id": ownedID}).Decode(&owned))
	assert.Equal(t, true, legacy["dynamic"])
	assert.Nil(t, legacy["scopes"], "no account-wide scopes left")
	assert.Nil(t, owned["dynamic"])
	assert.NotNil(t, owned["scopes"])
	assert.True(t, f.app.untrustedClient(ctx, "prn:::apps:/"+legacyID.Hex()))

	again, err := markLegacyDynamicClients(ctx, f.db)
	require.NoError(t, err)
	assert.Zero(t, again, "idempotent")
}

// pvr is built in: its login works on a deployment where nobody created the
// application, and nobody can create one to take its place.
func TestPvrLoginNeedsNoRegisteredApplication(t *testing.T) {
	f := newOAuthFixture(t)

	body, _ := json.Marshal(map[string]string{
		"client_id":             "prn:pantahub.com:apis:/pvr",
		"scope":                 scopeEverything,
		"redirect_uri":          "http://127.0.0.1:45123/callback",
		"code_challenge":        challengeFor(oauthTestVerifier),
		"code_challenge_method": "S256",
		"state":                 "s",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/pkce/init", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := f.do(req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, f.app.untrustedClient(context.Background(), "prn:pantahub.com:apis:/pvr"))

	// Its callbacks are loopback only.
	body, _ = json.Marshal(map[string]string{
		"client_id":             "prn:pantahub.com:apis:/pvr",
		"redirect_uri":          "https://evil.example.com/callback",
		"code_challenge":        challengeFor(oauthTestVerifier),
		"code_challenge_method": "S256",
	})
	req = httptest.NewRequest(http.MethodPost, "/auth/oauth/pkce/init", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	assert.Equal(t, http.StatusBadRequest, f.do(req).Code)
}

// An application created with nick pvr before it was built in keeps working
// exactly as stored.
func TestStoredPvrApplicationKeepsPrecedence(t *testing.T) {
	f := newOAuthFixture(t)
	ctx := context.Background()
	id := primitive.NewObjectID()
	_, err := f.db.Collection("pantahub_apps").InsertOne(ctx, bson.M{
		"_id": id, "prn": "prn:pantahub.com:apis:/pvr", "nick": "pvr", "name": "my pvr",
		"owner": "prn:pantahub.com:auth:/user1", "type": "pkce",
		"redirect_uris": bson.A{"http://127.0.0.1/mine"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Collection("pantahub_apps").DeleteOne(ctx, bson.M{"_id": id}) })

	app, _, err := apps.SearchApp(ctx, "", "prn:pantahub.com:apis:/pvr", f.db)
	require.NoError(t, err)
	assert.Equal(t, "my pvr", app.Name)
	assert.Equal(t, []string{"http://127.0.0.1/mine"}, app.RedirectURIs)

	assert.NoError(t, f.app.validateRedirectURI(ctx, "prn:pantahub.com:apis:/pvr", "http://127.0.0.1:4000/mine", redirecturi.AuditContext{}))
	assert.Error(t, f.app.validateRedirectURI(ctx, "prn:pantahub.com:apis:/pvr", "http://127.0.0.1:4000/callback", redirecturi.AuditContext{}),
		"the stored callbacks, not the built-in ones")
}
