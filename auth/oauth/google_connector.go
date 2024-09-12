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
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	googleClientID     = utils.GetEnv(utils.EnvGoogleOAuthClientID)
	googleClientSecret = utils.GetEnv(utils.EnvGoogleOAuthClientSecret)
	googleScopes       = []string{
		"https://www.googleapis.com/auth/userinfo.email",
	}
)

const oauthGoogleURLAPI = "https://www.googleapis.com/oauth2/v2/userinfo?access_token="

type googlePayload struct {
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
}

// GetGoogleConfig get configuration from return URL
func GetGoogleConfig() *oauth2.Config {
	port := utils.GetEnv(utils.EnvPantahubPort)
	url := fmt.Sprintf(
		"%s://%s/auth/oauth/callback/google",
		utils.GetEnv(utils.EnvPantahubScheme),
		utils.GetEnv(utils.EnvPantahubHost),
	)

	if port != "80" && port != "443" && port != "" {
		url = fmt.Sprintf(
			"%s://%s:%s/auth/oauth/callback/google",
			utils.GetEnv(utils.EnvPantahubScheme),
			utils.GetEnv(utils.EnvPantahubHost),
			utils.GetEnv(utils.EnvPantahubPort),
		)
	}
	return &oauth2.Config{
		RedirectURL:  url,
		ClientID:     googleClientID,
		ClientSecret: googleClientSecret,
		Scopes:       googleScopes,
		Endpoint:     google.Endpoint,
	}
}

// GoogleAuthorize use google to authorize user
func GoogleAuthorize(redirectURI string, config *oauth2.Config, w rest.ResponseWriter, r *rest.Request) {
	// Create oauthState cookie
	oauthState := generateStateOauthCookie(redirectURI, w)

	u := config.AuthCodeURL(oauthState)
	http.Redirect(w, r.Request, u, http.StatusTemporaryRedirect)
}

// GoogleCb use code to retrive service user data
func GoogleCb(ctx context.Context, config *oauth2.Config, code string) (payload *ResponsePayload, err error) {
	data, err := getUserDataFromGoogle(ctx, config, code)
	if err != nil {
		return &ResponsePayload{RedirectTo: ""}, err
	}
	googlePayload := &googlePayload{}
	err = json.Unmarshal(data, googlePayload)
	if err != nil {
		return &ResponsePayload{RedirectTo: ""}, err
	}

	if !googlePayload.VerifiedEmail {
		return &ResponsePayload{RedirectTo: ""}, errors.New("users email is not verified")
	}

	re := regexp.MustCompile(`@.*`)
	nick := fmt.Sprintf(
		"%s%d",
		re.ReplaceAllString(googlePayload.Email, ""),
		rand.Intn(100),
	)
	return &ResponsePayload{
		Email: googlePayload.Email,
		Nick:  nick,
		Raw:   string(data),
	}, nil
}

func getUserDataFromGoogle(ctx context.Context, config *oauth2.Config, code string) ([]byte, error) {
	// Use code to get token and get user info from Google.
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange wrong: %s", err.Error())
	}

	client := config.Client(ctx, token)
	response, err := client.Get(oauthGoogleURLAPI + token.AccessToken)
	// response, err := http.Get(oauthGoogleURLAPI + token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed getting user info: %s", err.Error())
	}
	defer response.Body.Close()

	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read response: %s", err.Error())
	}

	return contents, nil
}
