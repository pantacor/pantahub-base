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

package trails

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gitlab.com/pantacor/pantahub-base/auth"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/testutils"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"gopkg.in/resty.v1"
)

var (
	recorder        *httptest.ResponseRecorder
	server          *httptest.Server
	jwtMWA          *jwtauth.Config
	jwtMWR          *jwtauth.Config
	authURL         *url.URL
	serverURL       *url.URL
	devicesURL      *url.URL
	device          *devices.Device
	deviceAuthToken string
	userAuthToken   string
	step0Hash       string
)

func falseAuthenticator(userID string, password string) bool {
	return false
}

// IMPORTANT: you need a mongodb running localhost default port by default
func setUp(t *testing.T) {

	mongoClient, err := utils.GetMongoClientTest()

	if err != nil {
		t.Errorf("error getting mongoClient (%s)", err.Error())
		t.Fail()
	}

	// clean while ignore errors as usually this collection does not exist.
	mongoClient.Database(utils.MongoDb).Collection("pantahub_trails").Drop(nil)
	mongoClient.Database(utils.MongoDb).Collection("pantahub_devices").Drop(nil)
	mongoClient.Database(utils.MongoDb).Collection("pantahub_steps").Drop(nil)

	jwtMWA = &jwtauth.Config{
		Key:        []byte("secret key"),
		Realm:      "pantahub services",
		Timeout:    time.Minute * 60,
		MaxRefresh: time.Hour * 24,
	}

	jwtMWR = &jwtauth.Config{
		Key:   []byte("secret key"),
		Realm: "pantahub services",
	}

	recorder = httptest.NewRecorder()

	// auth app we need
	authApp := auth.New(jwtMWA, mongoClient)
	authEcho := echoutil.NewServer("test")
	authApp.Mount(authEcho)
	authServer := httptest.NewServer(authEcho.E)
	authURL, err = url.Parse(authServer.URL)
	if err != nil {
		t.Errorf("%s", "error parsing test server URL "+err.Error())
		t.Fail()
	}

	// trails app we test
	adminUsers := utils.GetSubscriptionAdmins()
	subService := subscriptions.NewService(mongoClient, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)
	devicesApp := devices.New(jwtMWR, subService, mongoClient)
	devicesEcho := echoutil.NewServer("test")
	devicesApp.Mount(devicesEcho)
	devicesServer := httptest.NewServer(devicesEcho.E)
	devicesURL, err = url.Parse(devicesServer.URL)
	if err != nil {
		t.Errorf("%s", "error parsing test server URL "+err.Error())
		t.Fail()
	}

	// trails app we test
	trailsApp := New(jwtMWR, mongoClient)
	srv := echoutil.NewServer("test")
	trailsApp.Mount(srv)
	server = httptest.NewServer(srv.E)
	serverURL, err = url.Parse(server.URL)
	if err != nil {
		t.Errorf("%s", "error parsing test server URL "+err.Error())
		t.Fail()
	}

	userAuthToken = testutils.DoLogin(t, authURL, "user1", "user1")
	device = testutils.CreateOwnedDevice(t, devicesURL, userAuthToken, "nick1", "secret1")
	deviceAuthToken = testutils.DoLogin(t, authURL, device.Prn, "secret1")
}

func tearDown(t *testing.T) {
}

func postState(t *testing.T) {
	u := *serverURL
	u.Path = "/trails/"

	res, err := resty.R().SetAuthToken(deviceAuthToken).SetBody(map[string]string{"mystate": "mystate"}).Post(u.String())

	if err != nil {
		t.Errorf("%s", "internal error calling test server "+err.Error())
		t.Fail()
	}

	var trail trailmodels.Trail
	err = json.Unmarshal(res.Body(), &trail)

	if err != nil {
		t.Errorf("%s", "internal error parsing trail"+err.Error())
		t.Fail()
	}
}

func postStateHash(t *testing.T) {

	s0 := *serverURL
	s0.Path = "/trails/" + device.ID.Hex() + "/steps/0"

	res, err := resty.R().SetAuthToken(userAuthToken).
		Get(s0.String())

	if err != nil {
		t.Errorf("%s", "internal error getting step 0"+err.Error())
		t.Fail()
	}

	var step trailmodels.Step
	err = json.Unmarshal(res.Body(), &step)

	if err != nil {
		t.Errorf("%s", "internal error parsing trail"+err.Error())
		t.Fail()
	}

	if step.StateSha == "" {
		t.Error("state sha is empty: " + string(res.Body()))
		t.Fail()
	}
	step0Hash = step.StateSha
}

func postStep(t *testing.T) {
	u := *serverURL
	u.Path = "/trails/" + device.ID.Hex() + "/steps"

	res, err := resty.R().SetAuthToken(userAuthToken).
		SetBody("{\"rev\": 1, \"state\": {\"mystate\":         \"mystate\"}}").
		Post(u.String())

	if err != nil {
		t.Errorf("%s", "internal error calling test server "+err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusOK {
		t.Error("posting step must return status OK.")
		t.Error(" -> " + string(res.Body()))
		t.Fail()
	}

	var step trailmodels.Step
	err = json.Unmarshal(res.Body(), &step)

	if err != nil {
		t.Errorf("%s", "internal error parsing trail: "+err.Error())
		t.Fail()
	}
}

func postStepsHash(t *testing.T) {

	s1 := *serverURL
	s1.Path = "/trails/" + device.ID.Hex() + "/steps/1"

	res, err := resty.R().SetAuthToken(userAuthToken).
		Get(s1.String())

	if err != nil {
		t.Errorf("%s", "internal error getting step 0"+err.Error())
		t.Fail()
	}

	var step trailmodels.Step
	err = json.Unmarshal(res.Body(), &step)

	if err != nil {
		t.Errorf("%s", "internal error parsing trail"+err.Error())
		t.Fail()
	}

	if step.StateSha == "" {
		t.Error("state sha is empty: " + string(res.Body()))
		t.Fail()
	}

	// test canonicalization feature of json
	if step.StateSha != step0Hash {
		t.Errorf("state shas of step 0 and 1 differ (%s != %s): %s", step0Hash, step.StateSha, string(res.Body()))
		t.Fail()
	}
}

func TestTrailsHash(t *testing.T) {
	setUp(t)

	t.Run("post state", postState)
	t.Run("post state hash", postStateHash)
	t.Run("post step", postStep)
	t.Run("post step Hash", postStepsHash)

	tearDown(t)
}
