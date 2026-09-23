//
// Copyright 2026 Pantacor Ltd.
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

package devices

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// envTestMongo points the tests that need a database at a disposable MongoDB.
const envTestMongo = "PANTAHUB_DEVICES_TEST_MONGO"

// Default accounts, so the handlers find the owner without an accounts row.
const (
	testOwnerPrn    = "prn:pantahub.com:auth:/user1"
	testStrangerPrn = "prn:pantahub.com:auth:/user2"
)

type publicDeviceFixture struct {
	app    *App
	device primitive.ObjectID
}

// newTestClient connects to the disposable MongoDB and points the package at a
// fresh database that is dropped afterwards.
func newTestClient(t *testing.T) *mongo.Client {
	t.Helper()
	uri := os.Getenv(envTestMongo)
	if uri == "" {
		t.Skip(envTestMongo + " is not set")
	}
	t.Setenv(utils.EnvFluentPort, "") // error responses would dial fluentd and exit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))

	previousDb := utils.MongoDb
	utils.MongoDb = fmt.Sprintf("devices_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		client.Database(utils.MongoDb).Drop(context.Background())
		utils.MongoDb = previousDb
		client.Disconnect(context.Background())
	})
	return client
}

// testRequest runs handler as a USER request of caller.
func testRequest(t *testing.T, handler func(*echo.Context) error, caller, method, target string, body io.Reader, params ...echo.PathValue) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Content-Type", "application/json")
	c := echo.New().NewContext(req, rec)
	c.Set(echoutil.KeyJWTPayload, jwtgo.MapClaims{"prn": caller, "type": "USER"})
	c.SetPathValues(append(echo.PathValues{}, params...))
	require.NoError(t, handler(c))
	return rec
}

// newPublicDeviceFixture stores one public device of testOwnerPrn with
// user-meta, device-meta and global profile meta.
func newPublicDeviceFixture(t *testing.T) *publicDeviceFixture {
	t.Helper()
	client := newTestClient(t)
	ctx := context.Background()

	f := &publicDeviceFixture{app: &App{mongoClient: client}, device: primitive.NewObjectID()}
	db := client.Database(utils.MongoDb)
	_, err := db.Collection("pantahub_devices").InsertOne(ctx, bson.M{
		"_id": f.device, "prn": "prn:::devices:/" + f.device.Hex(), "nick": "shared_one",
		"owner": testOwnerPrn, "ispublic": true, "secret_hash": "hash", "secret": "legacy-plaintext",
		"challenge": "challenge", "ownership_unverified": true, "mark_public_processed": true,
		"meta-modified": time.Now(), "ovmode": bson.M{"mode": "tls", "status": "pending", "root_of_trust": "cert"},
		"user-meta":   bson.M{"site": "lab"},
		"device-meta": bson.M{"pantavisor": bson.M{"version": "019"}},
	})
	require.NoError(t, err)
	_, err = db.Collection("pantahub_profiles").InsertOne(ctx, bson.M{
		"prn": testOwnerPrn, "meta": bson.M{"authorized_keys": "ssh-ed25519 AAAA"},
	})
	require.NoError(t, err)
	return f
}

func (f *publicDeviceFixture) request(t *testing.T, handler func(*echo.Context) error, caller, target string, params ...echo.PathValue) *httptest.ResponseRecorder {
	t.Helper()
	return testRequest(t, handler, caller, http.MethodGet, target, nil, params...)
}

func (f *publicDeviceFixture) getDevice(t *testing.T, caller string) Device {
	t.Helper()
	rec := f.request(t, f.app.handleGetDevice, caller,
		"/devices/"+f.device.Hex()+"?owner="+testOwnerPrn, echo.PathValue{Name: "id", Value: f.device.Hex()})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	device := Device{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &device))
	return device
}

func TestPublicDeviceMetaIsOnlyForTheOwner(t *testing.T) {
	f := newPublicDeviceFixture(t)

	stranger := f.getDevice(t, testStrangerPrn)
	assert.Empty(t, stranger.UserMeta, "neither the device's nor the owner's global meta")
	assert.Empty(t, stranger.DeviceMeta)
	assert.Equal(t, "shared_one", stranger.Nick, "the device itself is still public")

	owner := f.getDevice(t, testOwnerPrn)
	assert.Equal(t, map[string]any{"site": "lab", "authorized_keys": "ssh-ed25519 AAAA"}, owner.UserMeta)
	assert.NotEmpty(t, owner.DeviceMeta)
}

func TestPublicDeviceListCannotBeFilteredOnHiddenFields(t *testing.T) {
	f := newPublicDeviceFixture(t)

	for _, filter := range []string{"user-meta.site=lab", "device-meta.pantavisor.version=%5E0", "secret_hash=hash", "challenge=x"} {
		rec := f.request(t, f.app.handleGetDevices, testStrangerPrn, "/devices?owner="+testOwnerPrn+"&"+filter)
		assert.Equal(t, http.StatusBadRequest, rec.Code, filter)
	}

	rec := f.request(t, f.app.handleGetDevices, testStrangerPrn, "/devices?owner="+testOwnerPrn+"&nick=shared_one")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	listed := []Device{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 1, "other filters still work")
	assert.Empty(t, listed[0].UserMeta)

	rec = f.request(t, f.app.handleGetDevices, testOwnerPrn, "/devices?user-meta.site=lab")
	assert.Equal(t, http.StatusOK, rec.Code, "the owner may filter on their own metadata")

	rec = f.request(t, f.app.handleGetDevices, testOwnerPrn, "/devices?$where=sleep(1000)")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "operators are never filters")
}

// publicKeys is every JSON key a stranger may see of a public device.
var publicKeys = []string{"id", "prn", "nick", "owner", "owner-nick", "public", "time-created", "time-modified", "user-meta", "device-meta"}

func assertPublicView(t *testing.T, raw []byte, path string) {
	t.Helper()
	fields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(raw, &fields), path)
	for key, value := range fields {
		switch {
		case key == "user-meta" || key == "device-meta":
			assert.JSONEq(t, "{}", string(value), "%s leaks %s", path, key)
		case !slices.Contains(publicKeys, key):
			// Fields without omitempty stay in the shape, zeroed.
			assert.Contains(t, []string{"false", `"0001-01-01T00:00:00Z"`, `""`, "null"}, string(value), "%s leaks %s", path, key)
		}
	}
	assert.Contains(t, fields, "nick", path)
}

func TestPublicDeviceStrangersGetOnlyThePublicView(t *testing.T) {
	f := newPublicDeviceFixture(t)

	rec := f.request(t, f.app.handleGetDevice, testStrangerPrn,
		"/devices/"+f.device.Hex()+"?owner="+testOwnerPrn, echo.PathValue{Name: "id", Value: f.device.Hex()})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assertPublicView(t, rec.Body.Bytes(), "GET /devices/:id")

	rec = f.request(t, f.app.handleGetUserDevice, testStrangerPrn, "/devices/np/user1/shared_one",
		echo.PathValue{Name: "usernick", Value: "user1"}, echo.PathValue{Name: "devicenick", Value: "shared_one"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assertPublicView(t, rec.Body.Bytes(), "GET /devices/np/:usernick/:devicenick")

	rec = f.request(t, f.app.handleGetDevices, testStrangerPrn, "/devices?owner="+testOwnerPrn)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	listed := []json.RawMessage{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 1)
	assertPublicView(t, listed[0], "GET /devices")

	// The owner still gets everything but the secrets.
	rec = f.request(t, f.app.handleGetDevice, testOwnerPrn,
		"/devices/"+f.device.Hex(), echo.PathValue{Name: "id", Value: f.device.Hex()})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"ovmode"`)
	assert.NotContains(t, rec.Body.String(), "legacy-plaintext")
	assert.NotContains(t, rec.Body.String(), `"hash"`)
}
