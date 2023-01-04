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
	"net/http"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/gitlab"
)

var (
	gitlabClientID     = utils.GetEnv(utils.EnvGitlabOAuthClientID)
	gitlabClientSecret = utils.GetEnv(utils.EnvGitlabOAuthClientSecret)
	gitlabScopes       = []string{"read_user", "profile"}
)

const oauthGitlabURLAPI = "https://gitlab.com/api/v4/user?access_token="

type gitlabPayload struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	State    string `json:"state"`
}

// GetGitlabConfig get configuration from return URL
func GetGitlabConfig() *oauth2.Config {
	return &oauth2.Config{
		RedirectURL: fmt.Sprintf(
			"%s://%s/auth/oauth/callback/gitlab",
			utils.GetEnv(utils.EnvPantahubScheme),
			utils.GetEnv(utils.EnvPantahubHost),
		),
		ClientID:     gitlabClientID,
		ClientSecret: gitlabClientSecret,
		Scopes:       gitlabScopes,
		Endpoint:     gitlab.Endpoint,
	}
}

// GitlabAuthorize use google to authorize user
func GitlabAuthorize(redirectURI string, config *oauth2.Config, w rest.ResponseWriter, r *rest.Request) {
	// Create oauthState cookie
	oauthState := generateStateOauthCookie(redirectURI, w)

	u := config.AuthCodeURL(oauthState)
	http.Redirect(w, r.Request, u, http.StatusTemporaryRedirect)
}

// GitlabCb use code to retrive service user data
func GitlabCb(ctx context.Context, config *oauth2.Config, code string) (*ResponsePayload, error) {
	data, err := getUserDataFromGitlab(ctx, config, code)
	if err != nil {
		return nil, err
	}

	payload := &gitlabPayload{}
	err = json.Unmarshal(data, payload)
	if err != nil {
		return nil, err
	}

	if payload.State != "active" {
		return nil, fmt.Errorf("user is not active")
	}

	return &ResponsePayload{
		Email: payload.Email,
		Nick:  payload.Username,
		Raw:   string(data),
	}, nil
}

func getUserDataFromGitlab(ctx context.Context, config *oauth2.Config, code string) ([]byte, error) {
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange wrong: %s", err.Error())
	}

	response, err := http.Get(oauthGitlabURLAPI + token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed getting user info: %s", err.Error())
	}
	defer response.Body.Close()

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read response: %s", err.Error())
	}

	return contents, nil
}
