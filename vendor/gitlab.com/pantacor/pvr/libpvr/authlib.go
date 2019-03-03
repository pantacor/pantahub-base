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
package libpvr

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty"
)

type PvrAuthTarget struct {
	// Name is the name of the target; 'default' for api.pantahub.com
	Name     string
	TargetEp *url.URL
}

type PvrAuthTokens struct {
	Name string
	// Name is the name of the target; 'default' for api.pantahub.com
	AccessToken  string `json:"access-token"`
	RefreshToken string `json:"refresh-token"`
}

type PvrAuthConfig struct {
	Spec   string                   `json:"spec"`
	Tokens map[string]PvrAuthTokens `json:"tokens"`
	path   string
}

func LoadConfig(filePath string) (*PvrAuthConfig, error) {
	_, err := os.Stat(filePath)

	if os.IsNotExist(err) {
		_, err := os.Stat(filepath.Dir(filePath))
		if os.IsNotExist(err) {
			err = os.MkdirAll(filepath.Dir(filePath), 0700)
		} else {
			err = nil
		}

		return newDefaultAuthConfig(filePath), err
	}

	if err != nil {
		return nil, errors.New("OS error getting stats for file in LoadConfig: " + err.Error())
	}

	byteJson, err := ioutil.ReadFile(filePath)

	if err != nil {
		return nil, errors.New("OS error reading config file LoadConfig: " + err.Error())
	}

	var authConfig PvrAuthConfig

	err = json.Unmarshal(byteJson, &authConfig)
	if err != nil {
		return nil, errors.New("JSON Unmarshal error parsing config file in LoadConfig (" + filePath + "): " + err.Error())
	}

	authConfig.path = filePath
	return &authConfig, nil
}

func (p *PvrAuthConfig) DoRefresh(authEp, token string) (string, string, error) {
	m := map[string]string{
		"token": token,
	}

	if token == "" {
		return "", "", errors.New("DoRefresh: no token provided.")
	}
	if authEp == "" {
		return "", "", errors.New("doAuthenticate: no authentication endpoint provided.")
	}

	response, err := resty.R().SetBody(m).
		SetAuthToken(token).
		Get(authEp + "/login")

	m1 := map[string]interface{}{}
	err = json.Unmarshal(response.Body(), &m1)

	if err != nil {
		return "", "", err
	}

	if response.StatusCode() != 200 {
		return "", "", nil
	}

	return m1["token"].(string), m1["token"].(string), nil
}

func (p *PvrAuthConfig) Save() error {

	if p.path == "" {
		return errors.New("not persistent authconfig")
	}

	configNew := p.path + ".tmp"
	configPath := p.path

	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		d := filepath.Dir(configPath)
		dInfo, err := os.Stat(configPath)
		if os.IsNotExist(err) {
			// lets mkdir a personal dir if it does not exists.
			err = os.MkdirAll(d, 0700)
		} else if err != nil {
			return err
		} else {
			if !dInfo.IsDir() {

				return errors.New("pvr config directrory is not a directory, but a file: " + d)
			}
			// if directory exists and is a directory, we are happy to continue and
			// attempt write file...
		}
	} else if err != nil {
		// all other errors are bad news and we return
		return err
	}

	byteJson, err := json.MarshalIndent(p, "", "    ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(configNew, byteJson, 0600)
	if err != nil {
		return err
	}

	err = os.Rename(configNew, configPath)
	return err
}

func doAuthenticate(authEp, username, password string) (string, string, error) {

	m := map[string]string{
		"username": username,
		"password": password,
	}

	if username == "" {
		return "", "", errors.New("doAuthenticate: no username provided.")
	}
	if password == "" {
		return "", "", errors.New("doAuthenticate: no password provided.")
	}
	if authEp == "" {
		return "", "", errors.New("doAuthenticate: no authentication endpoint provided.")
	}

	response, err := resty.R().SetBody(m).
		Post(authEp + "/login")

	m1 := map[string]interface{}{}
	err = json.Unmarshal(response.Body(), &m1)

	if err != nil {
		return "", "", err
	}

	if response.StatusCode() != 200 {
		return "", "", errors.New("Failed to Login: " + string(response.Body()))
	}

	_, ok := m1["token"]

	if !ok {
		return "", "", errors.New("Illegal response: " + string(response.Body()))
	}
	return m1["token"].(string), m1["token"].(string), nil
}

func (p *PvrAuthConfig) getCachedAccessToken(authHeader string) (string, error) {

	// no auth header; nothing we can do magic here...
	if authHeader == "" {
		return "", errors.New("Bad Parameter (authHeader empty)")
	}

	authType, opts := getWwwAuthenticateInfo(authHeader)
	if authType != "JWT" && authType != "Bearer" {
		return "", errors.New("Invalid www-authenticate header retrieved")
	}

	realm := opts["realm"]
	authEpString := opts["ph-aeps"]
	authEps := strings.Split(authEpString, ",")

	if len(authEps) == 0 {
		return "", errors.New("Bad Server Behaviour. Need ph-aeps token in Www-Authenticate header. Check your server version")
	}

	authEp := authEps[0]

	_, ok := p.Tokens[authEp+" realm="+realm]
	if ok && p.Tokens[authEp+" realm="+realm].AccessToken != "" {
		return p.Tokens[authEp+" realm="+realm].AccessToken, nil
	}

	return "", nil
}

func (p *PvrAuthConfig) getNewAccessToken(authHeader string) (string, error) {

	authType, opts := getWwwAuthenticateInfo(authHeader)
	if authType != "JWT" && authType != "Bearer" {
		return "", errors.New("Invalid www-authenticate header retrieved")
	}

	realm := opts["realm"]
	authEpString := opts["ph-aeps"]
	authEps := strings.Split(authEpString, ",")

	if len(authEps) == 0 {
		return "", errors.New("Bad Server Behaviour. Need ph-aeps token in Www-Authenticate header. Check your server version")
	}

	authEp := authEps[0]

	s, ok := p.Tokens[authEp+" realm="+realm]
	if !ok {
		s = PvrAuthTokens{}
	}

	s.AccessToken = ""

	// if we have a refresh token
	if s.RefreshToken != "" {
		accessToken, refreshToken, err := p.DoRefresh(authEp, s.RefreshToken)

		if err != nil {
			return "", err
		}

		s.RefreshToken = refreshToken
		s.AccessToken = accessToken
		p.Tokens[authEp+" realm="+realm] = s
		p.Save()

		if accessToken != "" {
			return accessToken, nil
		}
	}

	var err error
	// get fresh user/pass auth
	for i := 0; i < 3; i++ {
		var accessToken, refreshToken string
		username, password := readCredentials(authEp + " (realm=" + realm + ")")
		if username == "REGISTER" {
			email, username, password := readRegistration(authEp + " (realm=" + realm + ")")
			err = DoRegister(authEp, email, username, password)

			if err != nil {
				log.Fatal("error registering with PH: " + err.Error())
				os.Exit(122)
			}
		}
		accessToken, refreshToken, err = doAuthenticate(authEp, username, password)

		if err != nil {
			continue
		}

		if accessToken != "" {
			s.AccessToken = accessToken
			s.RefreshToken = refreshToken
			p.Tokens[authEp+" realm="+realm] = s
			p.Save()

			return accessToken, nil
		}
	}

	return "", err
}

func newDefaultAuthConfig(filePath string) *PvrAuthConfig {
	return &PvrAuthConfig{
		Spec:   "1",
		Tokens: map[string]PvrAuthTokens{},
		path:   filePath,
	}
}
