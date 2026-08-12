// Copyright 2016-2020  Pantacor Ltd.
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

// Package oauth package to manage extensions of the oauth protocol for oauth oAuthProviders
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"golang.org/x/oauth2"
)

// Config oauth configuration
type Config struct {
}

// ResponsePayload oauth response payload
type ResponsePayload struct {
	Nick       string      `json:"nick"`
	Email      string      `json:"email"`
	ProviderID string      `json:"provider_id"`
	RedirectTo string      `json:"redirect_uri"`
	Raw        string      `json:"raw"`
	Service    ServiceType `json:"service_type"`
}

// ServiceType type of service
type ServiceType string

// GetServiceConfigFunc get service configuration
type GetServiceConfigFunc func() *oauth2.Config

// AuthorizeServiceFunc use service authorization method
type AuthorizeServiceFunc func(redirectURI string, config *oauth2.Config, w rest.ResponseWriter, r *rest.Request)

// CallbackServiceFunc use service authorization method
type CallbackServiceFunc func(ctx context.Context, config *oauth2.Config, code string) (*ResponsePayload, error)

const (
	// ServiceGoogle google service enum
	ServiceGoogle = ServiceType("google")

	// ServiceGithub github service enum
	ServiceGithub = ServiceType("github")

	// ServiceGitlab gitlab service enum
	ServiceGitlab = ServiceType("gitlab")

	// ServiceEntraid entraid service enum
	ServiceEntraid = ServiceType("entraid")

	oauthCookie = "oauthstate"
)

// ServicesConfigs get service config
var ServicesConfigs = map[ServiceType]GetServiceConfigFunc{
	ServiceGoogle:  GetGoogleConfig,
	ServiceGithub:  GetGithubConfig,
	ServiceGitlab:  GetGitlabConfig,
	ServiceEntraid: GetEntraidConfig,
}

// ServicesAutorize get service config
var ServicesAutorize = map[ServiceType]AuthorizeServiceFunc{
	ServiceGoogle:  GoogleAuthorize,
	ServiceGithub:  GithubAuthorize,
	ServiceGitlab:  GitlabAuthorize,
	ServiceEntraid: EntraidAuthorize,
}

// ServicesCallback callback process by service
var ServicesCallback = map[ServiceType]CallbackServiceFunc{
	ServiceGoogle:  GoogleCb,
	ServiceGithub:  GithubCb,
	ServiceGitlab:  GitlabCb,
	ServiceEntraid: EntraidCb,
}

// RedirectValidator reports whether redirectURI is an acceptable return target
// for the social login flow. It is supplied by the caller so this package stays
// free of configuration and database dependencies.
type RedirectValidator func(redirectURI string) error

// AuthorizeByService use service to autorize
func AuthorizeByService(w rest.ResponseWriter, r *rest.Request, validate RedirectValidator) {
	service := ServiceType(r.PathParam("service"))
	if service == "" {
		// Authenticated connect flows start at /connected-providers and carry
		// the selected service in the request body. The caller places it in the
		// query only while invoking this shared authorizer.
		service = ServiceType(r.Request.URL.Query().Get("service"))
	}
	redirectURI := r.Request.URL.Query().Get("redirect_uri")

	if _, found := ServicesConfigs[service]; !found {
		utils.RestError(w, nil, "We can't connect to that service", http.StatusForbidden)
		return
	}

	// The callback returns a signed-in user token in the fragment, so the
	// return target has to be our own web interface. This is checked before we
	// go anywhere near the identity provider.
	if redirectURI != "" {
		if err := validate(redirectURI); err != nil {
			utils.RestError(w, err, err.Error(), http.StatusBadRequest)
			return
		}
	}

	authorizeURL, err := AuthorizationURLByService(service, redirectURI, w)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r.Request, authorizeURL, http.StatusTemporaryRedirect)
}

// AuthorizationURLByService creates a provider authorization URL and pins its
// signed state to the current browser session. Callers that need to initiate
// OAuth from an authenticated API request can return this URL as JSON and let
// the browser navigate to it afterwards.
func AuthorizationURLByService(service ServiceType, redirectURI string, w http.ResponseWriter) (string, error) {
	getConfig, found := ServicesConfigs[service]
	if !found {
		return "", fmt.Errorf("we can't connect to service: %s", service)
	}
	state := generateStateOauthCookie(redirectURI, w)
	if state == "" {
		return "", errors.New("unable to create OAuth state")
	}
	return getConfig().AuthCodeURL(state), nil
}

// CbByService use service callback
func CbByService(r *rest.Request) (*ResponsePayload, error) {
	var err error
	service := ServiceType(r.PathParam("service"))
	getConfig, found := ServicesConfigs[service]
	if !found {
		payload := &ResponsePayload{RedirectTo: ""}
		return payload, fmt.Errorf("we can't connect to service: %s", service)
	}

	code := r.FormValue("code")
	payload, err := ServicesCallback[service](r.Context(), getConfig(), code)
	if err != nil {
		return payload, fmt.Errorf("%s error -- %s", service, err)
	}

	oauthState, err := r.Cookie(oauthCookie)
	if err != nil {
		payload := &ResponsePayload{RedirectTo: ""}
		return payload, fmt.Errorf("error reading cookie: %s", err)
	}

	// The state returned by the provider must be the one we issued for this
	// browser session.
	returnedState := r.FormValue("state")
	if subtleCompare(returnedState, oauthState.Value) != 1 {
		payload := &ResponsePayload{RedirectTo: ""}
		return payload, errors.New("we can't validate the state")
	}

	// The redirect target travels inside the signed state rather than in a
	// cookie of its own, so it cannot be swapped independently of the state.
	claims, err := decodeState(returnedState)
	if err != nil {
		payload := &ResponsePayload{RedirectTo: ""}
		return payload, fmt.Errorf("we can't validate the state: %s", err)
	}

	payload.RedirectTo = claims.RedirectURI
	payload.Service = service

	return payload, nil
}

// stateClaims is the payload carried inside the OAuth state parameter. Binding
// the flow parameters to the state means a tampered redirect target invalidates
// the signature instead of silently redirecting somewhere else.
type stateClaims struct {
	Nonce       string `json:"n"`
	RedirectURI string `json:"r,omitempty"`
	IssuedAt    int64  `json:"t"`
}

// stateTTL bounds how long an issued state stays acceptable.
const stateTTL = 15 * time.Minute

var (
	stateKeyOnce sync.Once
	stateKey     []byte
)

// stateSigningKey derives the HMAC key used to sign state values. The JWT
// secret is reused so every replica signs with the same key; if it is missing
// we fall back to a per-process key, which keeps flows working on a single
// instance and fails closed across replicas rather than signing with nothing.
func stateSigningKey() []byte {
	stateKeyOnce.Do(func() {
		secret := utils.GetEnv(utils.EnvPantahubJWTAuthSecret)
		if secret != "" {
			sum := sha256.Sum256([]byte("pantahub-oauth-state:" + secret))
			stateKey = sum[:]
			return
		}

		log.Printf("WARNING: %s is unset; OAuth state signing falls back to a per-process key", utils.EnvPantahubJWTAuthSecret)
		stateKey = make([]byte, 32)
		if _, err := rand.Read(stateKey); err != nil {
			log.Printf("CRITICAL: unable to generate an OAuth state signing key: %v", err)
		}
	})

	return stateKey
}

// encodeState serialises and signs the flow parameters into a single opaque
// state value of the form <payload>.<mac>.
func encodeState(claims stateClaims) (string, error) {
	encoded, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	payload := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, stateSigningKey())
	mac.Write([]byte(payload))

	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// decodeState verifies the signature and freshness of a state value and
// returns the parameters it carries.
func decodeState(state string) (*stateClaims, error) {
	payload, signature, found := strings.Cut(state, ".")
	if !found {
		return nil, errors.New("malformed state")
	}

	expected := hmac.New(sha256.New, stateSigningKey())
	expected.Write([]byte(payload))

	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, errors.New("malformed state signature")
	}

	if !hmac.Equal(provided, expected.Sum(nil)) {
		return nil, errors.New("state signature mismatch")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errors.New("malformed state payload")
	}

	claims := &stateClaims{}
	if err := json.Unmarshal(decoded, claims); err != nil {
		return nil, errors.New("malformed state payload")
	}

	if time.Since(time.Unix(claims.IssuedAt, 0)) > stateTTL {
		return nil, errors.New("state has expired")
	}

	return claims, nil
}

// subtleCompare reports 1 when the two values are equal, comparing in constant
// time so the state cookie cannot be probed byte by byte.
func subtleCompare(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	if hmac.Equal([]byte(a), []byte(b)) {
		return 1
	}
	return 0
}

// generateStateOauthCookie issues a signed state carrying the flow parameters
// and pins it to this browser session with a cookie.
func generateStateOauthCookie(redirectURL string, w http.ResponseWriter) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("CRITICAL: crypto/rand.Read failed while generating OAuth state: %v", err)
		return ""
	}

	state, err := encodeState(stateClaims{
		Nonce:       base64.RawURLEncoding.EncodeToString(b),
		RedirectURI: redirectURL,
		IssuedAt:    time.Now().Unix(),
	})
	if err != nil {
		log.Printf("CRITICAL: unable to encode OAuth state: %v", err)
		return ""
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookie,
		Value:    state,
		Expires:  time.Now().Add(stateTTL),
		Path:     "/",
		HttpOnly: true,
		Secure:   utils.GetEnv(utils.EnvPantahubScheme) == "https",
		SameSite: http.SameSiteLaxMode,
	})

	return state
}
