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

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const secretMarker = "never-returned"

// #nosec G101 -- a mongo collection name, not a credential
const deviceTokensCollection = "pantahub_devices_tokens"

// manageFixture adds, to the device fixture, a join token and an application
// for each of the two accounts. Everything secret carries secretMarker.
type manageFixture struct {
	*toolFixture
	ownToken, foreignToken, disabledToken primitive.ObjectID
	ownApp, foreignApp                    primitive.ObjectID
}

func newManageFixture(t *testing.T) *manageFixture {
	t.Helper()
	f := &manageFixture{
		toolFixture:   newToolFixture(t),
		ownToken:      primitive.NewObjectID(),
		foreignToken:  primitive.NewObjectID(),
		disabledToken: primitive.NewObjectID(),
		ownApp:        primitive.NewObjectID(),
		foreignApp:    primitive.NewObjectID(),
	}
	ctx := context.Background()
	db := f.service.store.mongoClient.Database(utils.MongoDb)
	now := time.Now().UTC()

	token := func(id primitive.ObjectID, nick, owner string, disabled bool) bson.M {
		return bson.M{
			"_id": id, "prn": "prn:::devices-tokens:/" + id.Hex(), "nick": nick, "owner": owner,
			"disabled": disabled, "tokensha": []byte(secretMarker), "token": secretMarker,
			"defaultusermeta": quoted(map[string]interface{}{"fleet.group": "lab"}),
			"timecreated":     now, "timemodified": now,
		}
	}
	_, err := db.Collection(deviceTokensCollection).InsertMany(ctx, []interface{}{
		token(f.ownToken, "factory_line", ownerPrn, false),
		token(f.disabledToken, "retired", ownerPrn, true),
		token(f.foreignToken, "theirs", strangerPrn, false),
	})
	require.NoError(t, err)

	app := func(id primitive.ObjectID, nick, owner string) bson.M {
		return bson.M{
			"_id": id, "name": "App " + nick, "nick": nick, "prn": "prn:pantahub.com:apis:/" + nick,
			"owner": owner, "type": apps.AppTypeConfidential,
			"secret_hash": secretMarker, "secret": secretMarker, "logo": "data:image/png;base64," + secretMarker,
			"redirect_uris": []string{"https://" + nick + ".example.com/callback"},
			"scopes":        []bson.M{{"id": "devices.readonly", "service": utils.PantahubServiceID}},
			"time-created":  now, "time-modified": now,
		}
	}
	_, err = db.Collection(apps.DBCollection).InsertMany(ctx, []interface{}{
		app(f.ownApp, "mine", ownerPrn),
		app(f.foreignApp, "theirs", strangerPrn),
	})
	require.NoError(t, err)

	return f
}

// callAs is call with the scopes of the caller's choosing.
func (f *toolFixture) callAs(t *testing.T, scopes []string, tool string, args map[string]interface{}, out interface{}) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": tool, "arguments": args},
	})
	require.NoError(t, err)

	rec := rpc(t, f.service, sign(t, testKey, userClaims(scopes...)), string(body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
			Content           []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response), rec.Body.String())
	if response.Result.IsError {
		require.NotEmpty(t, response.Result.Content)
		return response.Result.Content[0].Text, rec.Body.String()
	}
	require.NoError(t, json.Unmarshal(response.Result.StructuredContent, out), rec.Body.String())
	return "", rec.Body.String()
}

func everyManageScope() []string {
	return supportedScopes()
}

func TestUpdateUserMetaMergesAndNeverTouchesDeviceMeta(t *testing.T) {
	f := newManageFixture(t)

	var out updateUserMetaOutput
	problem, _ := f.callAs(t, everyManageScope(), toolUpdateUserMeta, map[string]interface{}{
		"device": "alpha_one",
		"set":    map[string]interface{}{"wifi.ssid": "lab-net", "site": "roof"},
	}, &out)
	require.Empty(t, problem)
	assert.Equal(t, map[string]interface{}{"site": "roof", "wifi.ssid": "lab-net"}, out.UserMeta, "a key with a dot round-trips")

	// A fresh value: decoding into the one above would merge into its map and
	// show the removed key as still there.
	var after updateUserMetaOutput
	problem, _ = f.callAs(t, everyManageScope(), toolUpdateUserMeta, map[string]interface{}{
		"device": f.own.Hex(), "remove": []string{"site"},
	}, &after)
	require.Empty(t, problem)
	assert.Equal(t, map[string]interface{}{"wifi.ssid": "lab-net"}, after.UserMeta, "removing one key keeps the others")

	var device getDeviceOutput
	require.Empty(t, f.call(t, toolGetDevice, map[string]interface{}{"device": "alpha_one"}, &device))
	assert.Equal(t, map[string]interface{}{"pantavisor.version": "019"}, device.DeviceMeta, "device-meta is the device's to write")
}

func TestUserMetaIncludesTheAccountGlobalMeta(t *testing.T) {
	f := newManageFixture(t)
	_, err := f.service.store.collection("pantahub_profiles").InsertMany(context.Background(), []interface{}{
		bson.M{"prn": ownerPrn, "meta": quoted(map[string]interface{}{"pvr-sdk.authorized_keys": "ssh-ed25519 AAAA", "site": "global", "region": "eu"})},
		bson.M{"prn": strangerPrn, "meta": quoted(map[string]interface{}{"theirs": "hidden"})},
	})
	require.NoError(t, err)

	var device getDeviceOutput
	require.Empty(t, f.call(t, toolGetDevice, map[string]interface{}{"device": "alpha_one"}, &device))
	assert.Equal(t, map[string]interface{}{"pvr-sdk.authorized_keys": "ssh-ed25519 AAAA", "site": "lab", "region": "eu"},
		device.UserMeta, "as GET /devices/:id serves it: the owner's global meta, the device's own keys winning")

	var out updateUserMetaOutput
	problem, _ := f.callAs(t, everyManageScope(), toolUpdateUserMeta, map[string]interface{}{
		"device": "alpha_one", "set": map[string]interface{}{"region": "us"},
	}, &out)
	require.Empty(t, problem)
	assert.Equal(t, map[string]interface{}{"pvr-sdk.authorized_keys": "ssh-ed25519 AAAA", "site": "lab", "region": "us"},
		out.UserMeta, "a device key overrides the global one")

	var after updateUserMetaOutput
	problem, _ = f.callAs(t, everyManageScope(), toolUpdateUserMeta, map[string]interface{}{
		"device": "alpha_one", "remove": []string{"region"},
	}, &after)
	require.Empty(t, problem)
	assert.Equal(t, "eu", after.UserMeta["region"], "removing the override falls back to the global value")
}

func TestUpdateUserMetaCannotReachSomebodyElsesDevice(t *testing.T) {
	f := newManageFixture(t)

	var out updateUserMetaOutput
	for _, ref := range []string{f.foreign.Hex(), "alpha_foreign", "alpha_deleted", "no_such_device"} {
		problem, _ := f.callAs(t, everyManageScope(), toolUpdateUserMeta,
			map[string]interface{}{"device": ref, "set": map[string]interface{}{"owned": "yes"}}, &out)
		assert.NotEmpty(t, problem, ref)
	}

	stored := bson.M{}
	require.NoError(t, f.service.store.collection(devicesCollection).
		FindOne(context.Background(), bson.M{"_id": f.foreign}).Decode(&stored))
	assert.NotContains(t, stored["user-meta"], "owned")
}

func TestDeviceTokensAreListedWithoutTheirSecret(t *testing.T) {
	f := newManageFixture(t)

	var listed listDeviceTokensOutput
	problem, raw := f.callAs(t, everyManageScope(), toolListDeviceTokens, map[string]interface{}{}, &listed)
	require.Empty(t, problem)
	require.Len(t, listed.Tokens, 1, "disabled and foreign tokens are not listed")
	assert.Equal(t, "factory_line", listed.Tokens[0].Nick)
	assert.Equal(t, map[string]interface{}{"fleet.group": "lab"}, listed.Tokens[0].DefaultUserMeta)
	assert.NotContains(t, raw, secretMarker)

	var one deviceTokenSummary
	problem, raw = f.callAs(t, everyManageScope(), toolGetDeviceToken, map[string]interface{}{"token": f.ownToken.Hex()}, &one)
	require.Empty(t, problem)
	assert.Equal(t, f.ownToken.Hex(), one.ID)
	assert.NotContains(t, raw, secretMarker)

	for _, ref := range []string{f.foreignToken.Hex(), f.disabledToken.Hex(), "not-an-id", primitive.NewObjectID().Hex()} {
		problem, _ := f.callAs(t, everyManageScope(), toolGetDeviceToken, map[string]interface{}{"token": ref}, &one)
		assert.NotEmpty(t, problem, ref)
	}
}

func TestUpdateDeviceToken(t *testing.T) {
	f := newManageFixture(t)

	var updated deviceTokenSummary
	problem, raw := f.callAs(t, everyManageScope(), toolUpdateDeviceToken, map[string]interface{}{
		"token": f.ownToken.Hex(), "nick": "  line_two  ",
		"default_user_meta": map[string]interface{}{"fleet.group": "prod-line"},
	}, &updated)
	require.Empty(t, problem)
	assert.Equal(t, "line_two", updated.Nick)
	assert.Equal(t, map[string]interface{}{"fleet.group": "prod-line"}, updated.DefaultUserMeta)
	assert.NotContains(t, raw, secretMarker)

	// The secret a device enrols with is untouched by an update.
	stored := bson.M{}
	require.NoError(t, f.service.store.collection(deviceTokensCollection).
		FindOne(context.Background(), bson.M{"_id": f.ownToken}).Decode(&stored))
	assert.Equal(t, secretMarker, stored["token"])
	assert.False(t, stored["disabled"].(bool), "nothing here can disable a token")

	for name, args := range map[string]map[string]interface{}{
		"somebody else's": {"token": f.foreignToken.Hex(), "nick": "mine_now"},
		"a disabled one":  {"token": f.disabledToken.Hex(), "nick": "back"},
		"nothing to do":   {"token": f.ownToken.Hex()},
		"an empty nick":   {"token": f.ownToken.Hex(), "nick": "   "},
	} {
		problem, _ := f.callAs(t, everyManageScope(), toolUpdateDeviceToken, args, &updated)
		assert.NotEmpty(t, problem, name)
	}

	require.NoError(t, f.service.store.collection(deviceTokensCollection).
		FindOne(context.Background(), bson.M{"_id": f.foreignToken}).Decode(&stored))
	assert.Equal(t, "theirs", stored["nick"])
}

func TestAppsAreListedWithoutSecretOrLogo(t *testing.T) {
	f := newManageFixture(t)

	var listed listAppsOutput
	problem, raw := f.callAs(t, everyManageScope(), toolListApps, map[string]interface{}{}, &listed)
	require.Empty(t, problem)
	require.Len(t, listed.Apps, 1, "somebody else's applications are not listed")
	app := listed.Apps[0]
	assert.Equal(t, "prn:pantahub.com:apis:/mine", app.ClientID)
	assert.True(t, app.HasSecret)
	assert.True(t, app.HasLogo)
	assert.Equal(t, []string{utils.PantahubServiceID + "/devices.readonly"}, app.Scopes)
	assert.NotContains(t, raw, secretMarker, "neither the secret, its hash nor the inline logo")

	for _, ref := range []string{f.ownApp.Hex(), "mine", "prn:pantahub.com:apis:/mine"} {
		var one appSummary
		problem, raw := f.callAs(t, everyManageScope(), toolGetApp, map[string]interface{}{"app": ref}, &one)
		require.Empty(t, problem, ref)
		assert.Equal(t, f.ownApp.Hex(), one.ID, ref)
		assert.NotContains(t, raw, secretMarker)
	}

	var one appSummary
	for _, ref := range []string{f.foreignApp.Hex(), "theirs", "prn:pantahub.com:apis:/theirs", "nope"} {
		problem, _ := f.callAs(t, everyManageScope(), toolGetApp, map[string]interface{}{"app": ref}, &one)
		assert.NotEmpty(t, problem, ref)
	}
}

func TestUpdateAppChangesOnlyNameAndCallbacks(t *testing.T) {
	f := newManageFixture(t)
	collection := f.service.store.mongoClient.Database(utils.MongoDb).Collection(apps.DBCollection)

	var updated appSummary
	problem, raw := f.callAs(t, everyManageScope(), toolUpdateApp, map[string]interface{}{
		"app": "mine", "name": "Fleet Dashboard",
		"redirect_uris": []string{"https://dash.example.com/cb", "http://localhost/cb"},
	}, &updated)
	require.Empty(t, problem)
	assert.Equal(t, "Fleet Dashboard", updated.Name)
	assert.Equal(t, []string{"https://dash.example.com/cb", "http://localhost/cb"}, updated.RedirectURIs)
	assert.NotContains(t, raw, secretMarker)

	// Identity and credentials are exactly what they were.
	stored := bson.M{}
	require.NoError(t, collection.FindOne(context.Background(), bson.M{"_id": f.ownApp}).Decode(&stored))
	assert.Equal(t, "mine", stored["nick"])
	assert.Equal(t, "prn:pantahub.com:apis:/mine", stored["prn"])
	assert.Equal(t, apps.AppTypeConfidential, stored["type"])
	assert.Equal(t, secretMarker, stored["secret_hash"], "an update cannot rotate or drop the secret")
	assert.Len(t, stored["scopes"], 1)

	for name, args := range map[string]map[string]interface{}{
		"somebody else's":       {"app": "theirs", "name": "Mine Now"},
		"nothing to do":         {"app": "mine"},
		"empty name":            {"app": "mine", "name": "  "},
		"no callbacks left":     {"app": "mine", "redirect_uris": []string{}},
		"a script as callback":  {"app": "mine", "redirect_uris": []string{"javascript:alert(1)"}},
		"trying to change type": {"app": "mine", "type": "public"},
	} {
		problem, _ := f.callAs(t, everyManageScope(), toolUpdateApp, args, &updated)
		assert.NotEmpty(t, problem, name)
	}

	require.NoError(t, collection.FindOne(context.Background(), bson.M{"_id": f.foreignApp}).Decode(&stored))
	assert.Equal(t, "App theirs", stored["name"])
	require.NoError(t, collection.FindOne(context.Background(), bson.M{"_id": f.ownApp}).Decode(&stored))
	assert.Equal(t, "Fleet Dashboard", stored["name"], "a refused update changed nothing")
}

// apps.UpdateApp is the store's counterpart of SearchApp: it only ever
// reaches the caller's own stored applications.
func TestUpdateAppOnlyReachesTheOwnersStoredApplication(t *testing.T) {
	f := newManageFixture(t)
	ctx := context.Background()
	db := f.service.store.mongoClient.Database(utils.MongoDb)

	updated, _, err := apps.UpdateApp(ctx, ownerPrn, "mine", bson.M{"name": "Renamed"}, db)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name, "returns the record as stored afterwards")
	assert.Equal(t, "prn:pantahub.com:apis:/mine", updated.Prn)

	for name, call := range map[string]struct{ owner, id string }{
		"somebody else's":     {ownerPrn, "theirs"},
		"no owner":            {"", "mine"},
		"a built-in client":   {ownerPrn, "pvr"},
		"built-in, no owner":  {"", "pvr"},
		"no such application": {ownerPrn, "missing"},
	} {
		_, code, err := apps.UpdateApp(ctx, call.owner, call.id, bson.M{"name": "Taken"}, db)
		assert.Error(t, err, name)
		assert.Equal(t, http.StatusNotFound, code, name)
	}

	_, code, err := apps.UpdateApp(ctx, ownerPrn, "mine", bson.M{}, db)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
}

// The challenge in front of a tool is a courtesy to the client. The tool is
// what guards the data, and it has to refuse on its own.
func TestChangingToolsRefuseAReadOnlyGrantOnTheirOwn(t *testing.T) {
	_, err := authorize(nil, toolUpdateUserMeta)
	assert.Error(t, err)
	for tool := range changingTools {
		assert.False(t, utils.MatchScope(toolScopes[tool], defaultScopes()), tool)
	}
}
