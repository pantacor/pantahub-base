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

	"gitlab.com/pantacor/pantahub-base/devices"

	"gopkg.in/resty.v1"
)

// CreateOwnedDevice returns deviceId of new test device
func CreateOwnedDevice(t *testing.T, serverURL *url.URL, ownerAuthToken string,
	nick string, secret string) *devices.Device {

	u := serverURL
	u.Path = "/devices/"

	res, err := resty.R().SetAuthToken(ownerAuthToken).SetBody(
		map[string]interface{}{
			"nick":   nick,
			"secret": secret,
		}).Post(u.String())

	if err != nil {
		t.Errorf("%s", "internal error calling test server "+err.Error())
		t.Fail()
	}

	if res.StatusCode() != 200 {
		t.Error("post device with valid auth token must yield 200")
		t.Error("Error Body: " + string(res.Body()))
		t.Fail()
	}

	var device devices.Device

	err = json.Unmarshal(res.Body(), &device)

	if err != nil {
		t.Errorf("%s", "Bad json returned from server for login "+err.Error())
		t.Fail()
	}

	if device.ID.Hex() == "" {
		t.Errorf("%s", "Body contained no id: "+string(res.Body()))
		t.Fail()
	}

	return &device
}
