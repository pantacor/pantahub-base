package trails

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/auth"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/testutils"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/resty.v1"
)

var (
	recorder        *httptest.ResponseRecorder
	server          *httptest.Server
	jwtMWA          *jwt.JWTMiddleware
	jwtMWR          *jwt.JWTMiddleware
	authUrl         *url.URL
	serverUrl       *url.URL
	devicesUrl      *url.URL
	device          *devices.Device
	deviceAuthToken string
	userAuthToken   string
	step0Hash       string
)

func falseAuthenticator(userId string, password string) bool {
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

	recorder = httptest.NewRecorder()

	// auth app we need
	authApp := auth.New(jwtMWA, mongoClient)
	authServer := httptest.NewServer(authApp.Api.MakeHandler())
	authUrl, err = url.Parse(authServer.URL)
	if err != nil {
		t.Errorf("error parsing test server URL " + err.Error())
		t.Fail()
	}

	// trails app we test
	devicesApp := devices.New(jwtMWR, mongoClient)
	devicesServer := httptest.NewServer(devicesApp.Api.MakeHandler())
	devicesUrl, err = url.Parse(devicesServer.URL)
	if err != nil {
		t.Errorf("error parsing test server URL " + err.Error())
		t.Fail()
	}

	// trails app we test
	trailsApp := New(jwtMWR, mongoClient)
	server = httptest.NewServer(trailsApp.Api.MakeHandler())
	serverUrl, err = url.Parse(server.URL)
	if err != nil {
		t.Errorf("error parsing test server URL " + err.Error())
		t.Fail()
	}

	userAuthToken = testutils.DoLogin(t, authUrl, "user1", "user1")
	device = testutils.CreateOwnedDevice(t, devicesUrl, userAuthToken, "nick1", "secret1")
	deviceAuthToken = testutils.DoLogin(t, authUrl, device.Prn, "secret1")
}

func tearDown(t *testing.T) {
}

func postState(t *testing.T) {
	u := *serverUrl
	u.Path = ""

	res, err := resty.R().SetAuthToken(deviceAuthToken).SetBody(map[string]string{"mystate": "mystate"}).Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	var trail Trail
	err = json.Unmarshal(res.Body(), &trail)

	if err != nil {
		t.Errorf("internal error parsing trail" + err.Error())
		t.Fail()
	}
}

func postStateHash(t *testing.T) {

	s0 := *serverUrl
	s0.Path = device.Id.Hex() + "/steps/0"

	res, err := resty.R().SetAuthToken(userAuthToken).
		Get(s0.String())

	if err != nil {
		t.Errorf("internal error getting step 0" + err.Error())
		t.Fail()
	}

	var step Step
	err = json.Unmarshal(res.Body(), &step)

	if err != nil {
		t.Errorf("internal error parsing trail" + err.Error())
		t.Fail()
	}

	if step.StateSha == "" {
		t.Error("state sha is empty: " + string(res.Body()))
		t.Fail()
	}
	step0Hash = step.StateSha
}

func postStep(t *testing.T) {
	u := *serverUrl
	u.Path = device.Id.Hex() + "/steps"

	res, err := resty.R().SetAuthToken(userAuthToken).
		SetBody("{\"rev\": 1, \"state\": {\"mystate\":         \"mystate\"}}").
		Post(u.String())

	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}

	if res.StatusCode() != http.StatusOK {
		t.Error("posting step must return status OK.")
		t.Error(" -> " + string(res.Body()))
		t.Fail()
	}

	var step Step
	err = json.Unmarshal(res.Body(), &step)

	if err != nil {
		t.Errorf("internal error parsing trail: " + err.Error())
		t.Fail()
	}
}

func postStepsHash(t *testing.T) {

	s1 := *serverUrl
	s1.Path = device.Id.Hex() + "/steps/1"

	res, err := resty.R().SetAuthToken(userAuthToken).
		Get(s1.String())

	if err != nil {
		t.Errorf("internal error getting step 0" + err.Error())
		t.Fail()
	}

	var step Step
	err = json.Unmarshal(res.Body(), &step)

	if err != nil {
		t.Errorf("internal error parsing trail" + err.Error())
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
