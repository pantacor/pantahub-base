package auth

/*
 * Copyright 2017  Alexander Sack <asac129@gmail.com>
 */

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"net/url"
	"pantahub-base/utils"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
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

	mgoSession, err := utils.GetMongoSession()

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

	jwtMWR = &jwt.JWTMiddleware{
		Key:   []byte("secret key"),
		Realm: "pantahub services",
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

func doLogin(t *testing.T) {

	u := *serverUrl
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

	authTokenUser1, ok = resMap["token"].(string)
	if !ok {
		t.Errorf("Body contained no token: " + string(res.Body()))
		t.Fail()
	}
}

func TestAuthBase(t *testing.T) {
	setUp(t)

	t.Run("No credentials 401", testNoCredsLogin401)
	t.Run("Wrong credentials 401", testBadCredsLogin401)
	t.Run("Good Login", testGoodLogin)

	doLogin(t)

	t.Run("Refresh Token", testRefreshToken)

	tearDown(t)
}

func testRefreshToken(t *testing.T) {
	u := *serverUrl
	u.Path = "/login"

	res, err := resty.R().SetAuthToken(authTokenUser1).Get(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != 200 {
		t.Errorf("login without username/password must yield 200")
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
