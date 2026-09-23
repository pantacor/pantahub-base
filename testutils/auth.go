// Copyright (c) 2017-2026 Pantacor Ltd.
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

package testutils

import (
	"encoding/json"
	"net/url"
	"testing"

	"gopkg.in/resty.v1"
)

// DoLogin test login method
func DoLogin(t *testing.T, serverURL *url.URL, username string, password string) string {

	u := serverURL
	u.Path = "/auth/login"

	res, err := resty.R().SetBody(map[string]string{
		"username": username,
		"password": password,
	}).Post(u.String())

	if err != nil {
		t.Errorf("%s", "internal error calling test server "+err.Error())
		t.Fail()
	}

	if res.StatusCode() != 200 {
		t.Errorf("login without username/password must yield 200")
		t.Fail()
	}

	var resMap map[string]interface{}

	err = json.Unmarshal(res.Body(), &resMap)

	if err != nil {
		t.Errorf("%s", "Bad json returned from server for login "+err.Error())
		t.Fail()
	}

	var ok bool

	token, ok := resMap["token"].(string)
	if !ok {
		t.Errorf("%s", "Body contained no token: "+string(res.Body()))
		t.Fail()
	}

	return token
}
