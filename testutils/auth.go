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
	u.Path = "/login"

	res, err := resty.R().SetBody(map[string]string{
		"username": username,
		"password": password,
	}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != 200 {
		t.Errorf("login without username/password must yield 200")
		t.Fail()
	}

	var resMap map[string]interface{}

	err = json.Unmarshal(res.Body(), &resMap)

	if err != nil {
		t.Errorf("Bad json returned from server for login " + err.Error())
		t.Fail()
	}

	var ok bool

	token, ok := resMap["token"].(string)
	if !ok {
		t.Errorf("Body contained no token: " + string(res.Body()))
		t.Fail()
	}

	return token
}
