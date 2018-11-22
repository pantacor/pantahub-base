//
// Copyright 2017  Pantacor Ltd.
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
package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"net/url"

	"gitlab.com/pantacor/pantahub-base/testutils"
	"gitlab.com/pantacor/pantahub-base/utils"

	"github.com/fundapps/go-json-rest-middleware-jwt"
	"github.com/go-resty/resty"
)

var (
	recorder       *httptest.ResponseRecorder
	server         *httptest.Server
	jwtMWA         *jwt.JWTMiddleware
	jwtMWR         *jwt.JWTMiddleware
	serverUrl      *url.URL
	authTokenUser1 string
)

func setUp(t *testing.T) {

	mgoSession, err := utils.GetMongoSessionTest()

	if err != nil {
		t.Errorf("error getting mgoSession (%s)", err.Error())
		t.Fail()
	}

	jwtMWA = &jwt.JWTMiddleware{
		Key:        []byte("secret key"),
		Realm:      "pantahub services",
		Timeout:    time.Minute * 60,
		MaxRefresh: time.Hour * 24,
	}

	authApp := New(jwtMWA, mgoSession)

	recorder = httptest.NewRecorder()
	server = httptest.NewServer(authApp.Api.MakeHandler())
	serverUrl, err = url.Parse(server.URL)

	if err != nil {
		t.Errorf("error parsing test server URL " + err.Error())
		t.Fail()
	}
}

func tearDown(t *testing.T) {
}

func testNoCredsLogin401(t *testing.T) {

	u := *serverUrl
	u.Path = "/login"

	res, err := resty.R().SetBody(map[string]string{}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != 401 {
		t.Errorf("login without username/password must yield 401")
	}
}

func testBadCredsLogin401(t *testing.T) {

	u := *serverUrl
	u.Path = "/login"

	res, err := resty.R().SetBody(map[string]string{
		"username": "NOTEXISTuser1",
		"password": "NOTRIGHTuser1",
	}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != 401 {
		t.Errorf("login without username/password must yield 401")
	}
}

func testGoodLogin(t *testing.T) {

	u := serverUrl
	u.Path = "/login"

	res, err := resty.R().SetBody(map[string]string{
		"username": "user1",
		"password": "user1",
	}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != 200 {
		t.Errorf("login without username/password must yield 401")
	}
}

func testRefreshToken(t *testing.T) {
	u := *serverUrl
	u.Path = "/login"

	res, err := resty.R().SetAuthToken(authTokenUser1).Get(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusOK {
		t.Errorf("refresh token login without username/password must yield OK (200)")
	}

	var resMap map[string]interface{}

	err = json.Unmarshal(res.Body(), &resMap)

	if err != nil {
		t.Errorf("Bad json returned from server for login " + err.Error())
		t.Fail()
	}

	var ok bool

	authTokenUser1, ok = resMap["token"].(string)
	if !ok {
		t.Errorf("Body contained no token: " + string(res.Body()))
		t.Fail()
	}
}

func TestAuthLogin(t *testing.T) {
	setUp(t)

	t.Run("No credentials 401", testNoCredsLogin401)
	t.Run("Wrong credentials 401", testBadCredsLogin401)
	t.Run("Good Login", testGoodLogin)

	authTokenUser1 = testutils.DoLogin(t, serverUrl, "user1", "user1")

	t.Run("Refresh Token", testRefreshToken)

	tearDown(t)
}
