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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
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

	oauthCookie    = "oauthstate"
	redirectCookie = "redirecturi"
)

// ServicesConfigs get service config
var ServicesConfigs = map[ServiceType]GetServiceConfigFunc{
	ServiceGoogle: GetGoogleConfig,
	ServiceGithub: GetGithubConfig,
	ServiceGitlab: GetGitlabConfig,
}

// ServicesAutorize get service config
var ServicesAutorize = map[ServiceType]AuthorizeServiceFunc{
	ServiceGoogle: GoogleAuthorize,
	ServiceGithub: GithubAuthorize,
	ServiceGitlab: GitlabAuthorize,
}

// ServicesCallback callback process by service
var ServicesCallback = map[ServiceType]CallbackServiceFunc{
	ServiceGoogle: GoogleCb,
	ServiceGithub: GithubCb,
	ServiceGitlab: GitlabCb,
}

// AuthorizeByService use service to autorize
func AuthorizeByService(w rest.ResponseWriter, r *rest.Request) {
	service := ServiceType(r.PathParam("service"))
	redirectURI := r.Request.URL.Query().Get("redirect_uri")

	getConfig, found := ServicesConfigs[service]
	if !found {
		utils.RestError(w, nil, "We can't connect to that service", http.StatusForbidden)
		return
	}

	ServicesAutorize[service](redirectURI, getConfig(), w, r)
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

	if r.FormValue("state") != oauthState.Value {
		payload := &ResponsePayload{RedirectTo: ""}
		return payload, errors.New("we can't validate the state")
	}

	redirectURI, _ := r.Cookie(redirectCookie)
	if redirectURI != nil {
		payload.RedirectTo = redirectURI.Value
	}

	payload.Service = service

	return payload, nil
}

func generateStateOauthCookie(redirectURL string, w http.ResponseWriter) string {
	var expiration = time.Now().Add(365 * 24 * time.Hour)

	b := make([]byte, 16)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)

	cookie := &http.Cookie{
		Name:    oauthCookie,
		Value:   state,
		Expires: expiration,
		Path:    "/",
	}

	redirectURICookie := &http.Cookie{
		Name:    redirectCookie,
		Value:   redirectURL,
		Expires: expiration,
		Path:    "/",
	}

	http.SetCookie(w, cookie)
	http.SetCookie(w, redirectURICookie)

	return state
}
