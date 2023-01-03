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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var (
	githubClientID     = utils.GetEnv(utils.EnvGithubOAuthClientID)
	githubClientSecret = utils.GetEnv(utils.EnvGithubOAuthClientSecret)
	githubScopes       = []string{"user"}
)

const oauthGithubURLAPI = "https://api.github.com/user"

type githubPayload struct {
	Login string `json:"login"`
	Email string `json:"email"`
}

// GetGithubConfig get configuration from return URL
func GetGithubConfig() *oauth2.Config {
	return &oauth2.Config{
		RedirectURL: fmt.Sprintf(
			"%s://%s/auth/oauth/callback/github",
			utils.GetEnv(utils.EnvPantahubScheme),
			utils.GetEnv(utils.EnvPantahubHost),
		),
		ClientID:     githubClientID,
		ClientSecret: githubClientSecret,
		Scopes:       githubScopes,
		Endpoint:     github.Endpoint,
	}
}

// GithubAuthorize use google to authorize user
func GithubAuthorize(redirectURI string, config *oauth2.Config, w rest.ResponseWriter, r *rest.Request) {
	// Create oauthState cookie
	oauthState := generateStateOauthCookie(redirectURI, w)

	u := config.AuthCodeURL(oauthState)
	http.Redirect(w, r.Request, u, http.StatusTemporaryRedirect)
}

// GithubCb use code to retrive service user data
func GithubCb(ctx context.Context, config *oauth2.Config, code string) (*ResponsePayload, error) {
	data, err := getUserDataFromGithub(ctx, config, code)
	if err != nil {
		return nil, err
	}

	payload := &githubPayload{}
	err = json.Unmarshal(data, payload)
	if err != nil {
		return nil, err
	}

	return &ResponsePayload{
		Email: payload.Email,
		Nick:  payload.Login,
		Raw:   string(data),
	}, nil
}

func getUserDataFromGithub(ctx context.Context, config *oauth2.Config, code string) ([]byte, error) {
	// Use code to get token and get user info from Github.
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange wrong: %s", err.Error())
	}

	request, err := http.NewRequest(http.MethodGet, oauthGithubURLAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %s", err.Error())
	}

	request.Header.Set("Authorization", fmt.Sprintf(" token %s", token.AccessToken))

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   60 * time.Minute,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: 15 * time.Second,
	}

	httpClient := &http.Client{Transport: transport}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		httpClient = &http.Client{Transport: otelhttp.NewTransport(transport)}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed getting user info: %s", err.Error())
	}
	defer response.Body.Close()

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read response: %s", err.Error())
	}

	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("can't get account information: %s", contents)
	}

	return contents, nil
}
