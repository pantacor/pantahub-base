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

// Package auth package to manage extensions of the oauth protocol
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

	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
)

var (
	recorder         *httptest.ResponseRecorder
	server           *httptest.Server
	jwtMWA           *jwt.JWTMiddleware
	jwtMWR           *jwt.JWTMiddleware
	serverURL        *url.URL
	authTokenUser1   string
	authTokenClient1 string
)

func setUp(t *testing.T) {

	mongoClient, err := utils.GetMongoClientTest()

	if err != nil {
		t.Errorf("error getting mongoClient (%s)", err.Error())
		t.Fail()
	}

	jwtMWA = &jwt.JWTMiddleware{
		Key:        []byte("secret key"),
		Realm:      "pantahub services",
		Timeout:    time.Minute * 60,
		MaxRefresh: time.Hour * 24,
	}

	authApp := New(jwtMWA, mongoClient)

	recorder = httptest.NewRecorder()
	server = httptest.NewServer(authApp.API.MakeHandler())
	serverURL, err = url.Parse(server.URL)

	if err != nil {
		t.Errorf("error parsing test server URL " + err.Error())
		t.Fail()
	}
}

func tearDown(t *testing.T) {
}

func testNoCredsLogin401(t *testing.T) {

	u := *serverURL
	u.Path = "/login"

	res, err := utils.R().SetBody(map[string]string{}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != 401 {
		t.Errorf("login without username/password must yield 401")
	}
}

func testBadCredsLogin401(t *testing.T) {

	u := *serverURL
	u.Path = "/login"

	res, err := utils.R().SetBody(map[string]string{
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

	u := serverURL
	u.Path = "/login"

	res, err := utils.R().SetBody(map[string]string{
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
	u := *serverURL
	u.Path = "/login"

	res, err := utils.R().SetAuthToken(authTokenUser1).Get(u.String())

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

	authTokenUser1 = testutils.DoLogin(t, serverURL, "user1", "user1")
	t.Run("Refresh Token", testRefreshToken)

	tearDown(t)
}

func testAuthAuthTokenGood(t *testing.T) {
	u := *serverURL
	u.Path = "/authorize"

	body := map[string]interface{}{}

	body["service"] = "prn:pantahub.com:auth:/client1"
	body["scopes"] = "*"
	body["redirect_uri"] = "http://localhost:8081"

	res, err := utils.R().SetAuthToken(authTokenUser1).SetBody(&body).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusOK {
		t.Errorf("request implicit access token (oauth2) must yield OK (200)")
	}

	result := map[string]interface{}{}
	err = json.Unmarshal(res.Body(), &result)
	if err != nil {
		t.Errorf("error parsing body result as json" + err.Error())
		t.Fail()
	}

	// check all mandatory fields are in response json
	_, ok := result["state"]
	if !ok {
		t.Errorf("implicit /authorize must include 'state' in response")
	}
	_, ok = result["token"]
	if !ok {
		t.Errorf("implicit /authorize must include 'token' in response")
	}
	_, ok = result["token_type"]
	if !ok {
		t.Errorf("implicit /authorize must include 'token' in response")
	}
	_, ok = result["scopes"]
	if !ok {
		t.Errorf("implicit /authorize must include 'scopes' in response")
	}

	// check that redirect_uri has proper format and fields
	uriStr := result["redirect_uri"].(string)
	uri, err := url.Parse(uriStr)

	// protect against bad format
	if err != nil {
		t.Errorf("error parsing redirect_uri" + err.Error())
		t.Fail()
	}

	uriToken := uri.Query().Get("access_token")
	if uriToken == "" {
		t.Errorf("'access_token' field must be included in redirect_uri: redirect_uri=" + uriStr)
		t.Fail()
	}
	uriScope := uri.Query().Get("scope")
	if uriScope == "" {
		t.Errorf("'scope' field must be included in redirect_uri: redirect_uri=" + uriStr)
		t.Fail()
	}
	uriTokenType := uri.Query().Get("token_type")
	if uriTokenType == "" {
		t.Errorf("'token_type' field must be included in redirect_uri: redirect_uri=" + uriStr)
		t.Fail()
	}
	uriExpiresIn := uri.Query().Get("expires_in")
	if uriExpiresIn == "" {
		t.Errorf("'expires_in' field must be included in redirect_uri: redirect_uri=" + uriStr)
		t.Fail()
	}
}

func testAuthAuthTokenBadURL(t *testing.T) {
	u := *serverURL
	u.Path = "/authorize"

	body := map[string]interface{}{}

	body["service"] = "prn:pantahub.com:auth:/client1"
	body["scopes"] = "*"
	body["redirect_uri"] = "http://something.unknown"

	res, err := utils.R().SetAuthToken(authTokenUser1).SetBody(&body).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusBadRequest {
		t.Errorf("request implicit access token (oauth2) with URL not matching client must yield BadRequest (400)")
	}
}

func testAuthAuthTokenBadClient(t *testing.T) {
	u := *serverURL
	u.Path = "/authorize"

	body := map[string]interface{}{}

	body["service"] = "prn:pantahub.com:auth:/client1NONEXIST"
	body["scopes"] = "*"
	body["redirect_uri"] = "http://localhost:8081"

	res, err := utils.R().SetAuthToken(authTokenUser1).SetBody(&body).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusBadRequest {
		t.Errorf("request implicit access token (oauth2) with client not matching registered client must yield BadRequest (400)")
	}
}

func testAuthAuthTokenPreservesState(t *testing.T) {
	u := *serverURL
	u.Path = "/authorize"

	body := map[string]interface{}{}

	body["service"] = "prn:pantahub.com:auth:/client1"
	body["scopes"] = "*"
	body["redirect_uri"] = "http://localhost:8081"
	body["state"] = "MYSTATEHERE"

	res, err := utils.R().SetAuthToken(authTokenUser1).SetBody(&body).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusOK {
		t.Errorf("request implicit access token (oauth2) with client not matching registered client must yield BadRequest (400)")
	}

	result := map[string]interface{}{}
	err = json.Unmarshal(res.Body(), &result)
	if err != nil {
		t.Errorf("error parsing body result as json" + err.Error())
		t.Fail()
	}
	uriStr := result["redirect_uri"].(string)
	uri, err := url.Parse(uriStr)

	if err != nil {
		t.Errorf("error parsing redirect_uri" + err.Error())
		t.Fail()
	}

	resultState := uri.Query().Get("state")
	if resultState != body["state"].(string) {
		t.Errorf("'state' field of result does not match 'state' passed to /authorize endpoint:" + resultState + "!=" + body["state"].(string))
		t.Fail()
	}
}

func testAuthAuthTokenClientUse(t *testing.T) {
	u := *serverURL
	u.Path = "/auth_status"
	res, err := utils.R().SetAuthToken(authTokenClient1).Get(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusOK {
		t.Errorf("using implicit access token to retrieve auth_status must not fail with code != 200")
		t.Fail()
	}

	var result map[string]interface{}
	err = json.Unmarshal(res.Body(), &result)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	prn, ok := result["prn"]

	if !ok {
		t.Errorf("/auth_status with oauth2 implicit access token must return json with 'prn' field")
		t.Fail()
	}

	if prn != "prn:pantahub.com:auth:/user1" {
		t.Errorf("/auth_status with oauth2 implicit access token must 'prn' field matching 'prn:pantahub.com:auth:/client1', but returned: " + prn.(string))
		t.Fail()
	}
}

func TestOauth2Implicit(t *testing.T) {
	setUp(t)

	authTokenUser1 = testutils.DoLogin(t, serverURL, "user1", "user1")
	t.Run("Get Authorize Token (Good) ", testAuthAuthTokenGood)
	t.Run("Get Authorize Token (Bad URL) ", testAuthAuthTokenBadURL)
	t.Run("Get Authorize Token (Bad Client) ", testAuthAuthTokenBadClient)
	t.Run("Get Authorize Token (Preserve State) ", testAuthAuthTokenPreservesState)

	// authTokenClient1 = testutils.DoAuthorizeToken(t, serverUrl, authTokenUser1,
	// 	"prn:pantahub.com:auth:/client1", "*")
	t.Run("User Client 1 implicit auth token", testAuthAuthTokenClientUse)

	tearDown(t)
}
