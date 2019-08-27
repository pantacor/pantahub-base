//
// Copyright 2019  Pantacor Ltd.
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
package utils

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiURL = "https://www.google.com/recaptcha/api/siteverify"
)

type verifyResponse struct {
	Success     bool      `json:"success"`
	ChallengeTs time.Time `json:"challenge_ts"` // timestamp of the challenge load (ISO format yyyy-MM-dd'T'HH:mm:ssZZ)
	Hostname    string    `json:"hostname"`     // the hostname of the site where the reCAPTCHA was solved
	ErrorCodes  []string  `json:"error-codes"`  // optional
}

// VerifyReCaptchaToken validate a recaptcha token with google recaptcha API
func VerifyReCaptchaToken(token string) (bool, error) {
	resp, err := http.PostForm(apiURL, url.Values{"response": {token}, "secret": {GetEnv(ENV_GOOGLE_CAPTCHA_SECRET)}})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var data verifyResponse

	json.Unmarshal(body, &data)
	if len(data.ErrorCodes) > 0 {
		return false, errors.New(strings.Join(data.ErrorCodes, ", "))
	}

	return data.Success, nil
}
