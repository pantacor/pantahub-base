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
	u.Path = "/"

	res, err := resty.R().SetAuthToken(ownerAuthToken).SetBody(
		map[string]interface{}{
			"nick":   nick,
			"secret": secret,
		}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
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
		t.Errorf("Bad json returned from server for login " + err.Error())
		t.Fail()
	}

	if device.ID.Hex() == "" {
		t.Errorf("Body contained no id: " + string(res.Body()))
		t.Fail()
	}

	return &device
}
