//
// Copyright 2018  Pantacor Ltd.
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
package libpvr

import (
	"errors"
	"os/user"
	"path/filepath"

	"github.com/go-resty/resty"
	"github.com/urfave/cli"
)

func GetConfigDir() (string, error) {

	user, err := user.Current()
	if err != nil {
		return "", err
	}

	return filepath.Join(user.HomeDir, ".pvr"), nil
}

type Session struct {
	app  *cli.App
	auth *PvrAuthConfig
}

func NewSession(app *cli.App) (*Session, error) {
	configDir, err := GetConfigDir()

	if err != nil {
		return nil, errors.New("Cannot get config dir: " + err.Error())
	}

	configPath := filepath.Join(configDir, "auth.json")
	authConfig, err := LoadConfig(configPath)

	if err != nil {
		return nil, errors.New("Cannot load config: " + err.Error())
	}

	return &Session{
		app:  app,
		auth: authConfig,
	}, nil
}

func (s *Session) GetApp() *cli.App {
	return s.app
}

func (s *Session) DoAuthCall(fn WrappableRestyCallFunc) (*resty.Response, error) {

	var bearer string
	var err error
	var response *resty.Response

	// legacy flat -a from CLI will give a default token
	bearer = s.GetApp().Metadata["PVR_AUTH"].(string)
	response, err = fn(resty.R().SetAuthToken(bearer))

	// if we see www-authenticate, we need to auth ...
	authHeader := response.Header().Get("www-authenticate")

	// first try cached accesstoken
	if authHeader != "" {
		bearer, err = s.auth.getCachedAccessToken(authHeader)
		if bearer != "" {
			response, err = fn(resty.R().SetAuthToken(bearer))
			authHeader = response.Header().Get("Www-Authenticate")
		}
	}

	// then get new accesstoken
	if authHeader != "" {
		bearer, err = s.auth.getNewAccessToken(authHeader)
		if bearer != "" {
			response, err = fn(resty.R().SetAuthToken(bearer))
			authHeader = response.Header().Get("Www-Authenticate")
		}
	}

	return response, err
}
