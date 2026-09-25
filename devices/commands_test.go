//
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
//

package devices

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestValidateCommandRequest(t *testing.T) {
	args, err := ValidateCommandRequest(DeviceCommandRequest{Cmd: "REBOOT_DEVICE", Args: map[string]interface{}{"message": "maintenance", "force": true}})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"message": "maintenance"}, args, "only the message is kept")

	args, err = ValidateCommandRequest(DeviceCommandRequest{Cmd: "REBOOT_DEVICE"})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{}, args, "the message is optional")

	args, err = ValidateCommandRequest(DeviceCommandRequest{Cmd: "LIST_CONTAINERS", Args: map[string]interface{}{"message": "x", "path": "/"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{}, args, "commands without arguments drop them")

	for _, req := range []DeviceCommandRequest{
		{Cmd: ""},
		{Cmd: "list_containers"},
		{Cmd: "RUN_SHELL"},
		{Cmd: "REBOOT_DEVICE", Args: map[string]interface{}{"message": 42}},
		{Cmd: "REBOOT_DEVICE", Args: map[string]interface{}{"message": strings.Repeat("x", maxCommandMessageLength+1)}},
	} {
		_, err := ValidateCommandRequest(req)
		assert.Error(t, err, "%+v", req)
	}
}

// The allowlist is the contract's table, exactly.
func TestDeviceCommandsAllowlist(t *testing.T) {
	want := []string{"REBOOT_DEVICE", "RUN_GC", "ENABLE_SSH", "DISABLE_SSH", "LIST_CONTAINERS",
		"LIST_GROUPS", "LIST_DAEMONS", "LIST_DRIVERS", "LIST_WAKELOCKS", "GET_XCONNECT_GRAPH"}
	assert.Len(t, DeviceCommands, len(want))
	for _, cmd := range want {
		takesMessage, ok := DeviceCommands[cmd]
		assert.True(t, ok, cmd)
		assert.Equal(t, cmd == "REBOOT_DEVICE", takesMessage, cmd)
	}
}

func TestEffectiveCommandStatus(t *testing.T) {
	created := time.Now()
	assert.Equal(t, "pending", EffectiveCommandStatus("pending", created, created.Add(CommandTimeout)))
	assert.Equal(t, "timeout", EffectiveCommandStatus("pending", created, created.Add(CommandTimeout+time.Second)))
	assert.Equal(t, "ok", EffectiveCommandStatus("ok", created, created.Add(time.Hour)), "finished commands keep their status")
	assert.Equal(t, "rejected", EffectiveCommandStatus("rejected", created, created.Add(time.Hour)))
}

func TestParseCommandsLimit(t *testing.T) {
	for raw, want := range map[string]int{"": 20, "1": 1, "100": 100, "101": 100, "5000": 100} {
		got, err := parseCommandsLimit(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"0", "-1", "ten", "1.5"} {
		_, err := parseCommandsLimit(raw)
		assert.Error(t, err, raw)
	}
}

// Output round-trips through storage as the JSON the device sent, including
// keys Mongo could not store verbatim.
func TestCommandOutputRoundTrip(t *testing.T) {
	for _, raw := range []string{
		`[{"name":"os","status":"STARTED"}]`,
		`{"lo.ipv4":"127.0.0.1","$where":"x","nested":{"a.b":[1,2.5,"$c"]}}`,
		`"a plain string with a . and a $"`,
		`true`,
		`null`,
	} {
		var output interface{}
		require.NoError(t, json.Unmarshal([]byte(raw), &output))

		doc, err := bson.Marshal(bson.M{"output": EncodeCommandOutput(output)})
		require.NoError(t, err, raw)
		stored := struct {
			Output bson.RawValue `bson:"output"`
		}{}
		require.NoError(t, bson.Unmarshal(doc, &stored), raw)

		got, err := json.Marshal(DecodeCommandOutput(stored.Output))
		require.NoError(t, err, raw)
		assert.JSONEq(t, raw, string(got))
	}

	assert.Nil(t, DecodeCommandOutput(bson.RawValue{}), "missing output is null")
}

func TestMqttConnectedReadsTheQuotedKey(t *testing.T) {
	assert.True(t, mqttConnected(map[string]interface{}{"pantahubＮmqttＮconnected": true}))
	assert.False(t, mqttConnected(map[string]interface{}{"pantahubＮmqttＮconnected": false}))
	assert.False(t, mqttConnected(map[string]interface{}{"pantahubＮmqttＮconnected": "true"}))
	assert.False(t, mqttConnected(map[string]interface{}{"pantahubＮonline": true}))
	assert.False(t, mqttConnected(nil))
}

type commandFixture struct {
	app       *App
	connected primitive.ObjectID
	offline   primitive.ObjectID
}

// newCommandFixture stores two devices of testOwnerPrn: one the MQTT bridge
// recorded as connected, one it recorded as disconnected.
func newCommandFixture(t *testing.T) *commandFixture {
	t.Helper()
	client := newTestClient(t)
	f := &commandFixture{app: &App{mongoClient: client}, connected: primitive.NewObjectID(), offline: primitive.NewObjectID()}

	devices := client.Database(utils.MongoDb).Collection("pantahub_devices")
	for id, connected := range map[primitive.ObjectID]bool{f.connected: true, f.offline: false} {
		meta := map[string]interface{}{DeviceMetaMqttConnected: connected}
		_, err := devices.InsertOne(context.Background(), bson.M{
			"_id": id, "prn": "prn:::devices:/" + id.Hex(), "nick": "dev_" + id.Hex(), "owner": testOwnerPrn,
			"device-meta": utils.BsonQuoteMap(&meta),
		})
		require.NoError(t, err)
	}
	return f
}

func (f *commandFixture) post(t *testing.T, caller string, device primitive.ObjectID, body string) (int, DeviceCommandView, string) {
	t.Helper()
	rec := testRequest(t, f.app.handlePostCommand, caller, http.MethodPost, "/devices/"+device.Hex()+"/commands",
		strings.NewReader(body), echo.PathValue{Name: "id", Value: device.Hex()})
	view := DeviceCommandView{}
	if rec.Code == http.StatusCreated {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	}
	return rec.Code, view, rec.Body.String()
}

func TestPostCommand(t *testing.T) {
	f := newCommandFixture(t)

	code, view, body := f.post(t, testOwnerPrn, f.connected, `{"cmd":"REBOOT_DEVICE","args":{"message":"bye","extra":1}}`)
	require.Equal(t, http.StatusCreated, code, body)
	assert.Equal(t, f.connected.Hex(), view.DeviceID)
	assert.Equal(t, "REBOOT_DEVICE", view.Cmd)
	assert.Equal(t, map[string]interface{}{"message": "bye"}, view.Args)
	assert.Equal(t, "pending", view.Status)
	assert.Equal(t, testOwnerPrn, view.CreatedBy)
	assert.Equal(t, CommandExpiry, view.ExpiresAt.Sub(view.CreatedAt))
	assert.Nil(t, view.FinishedAt)
	assert.WithinDuration(t, time.Now(), view.CreatedAt, 5*time.Second)
	for _, key := range []string{`"id"`, `"device_id"`, `"cmd"`, `"args"`, `"status"`, `"code"`, `"output"`, `"error"`, `"created_at"`, `"expires_at"`, `"finished_at"`, `"created_by"`} {
		assert.Contains(t, body, key)
	}
	assert.NotContains(t, body, `"owner"`)

	stored := DeviceCommand{}
	id, err := primitive.ObjectIDFromHex(view.ID)
	require.NoError(t, err)
	require.NoError(t, f.app.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection).
		FindOne(context.Background(), bson.M{"_id": id}).Decode(&stored))
	assert.Equal(t, testOwnerPrn, stored.Owner)
	assert.Equal(t, "pending", stored.Status)

	code, _, body = f.post(t, testOwnerPrn, f.connected, `{"cmd":"RUN_SHELL"}`)
	assert.Equal(t, http.StatusBadRequest, code, body)

	code, _, body = f.post(t, testOwnerPrn, f.connected, `not json`)
	assert.Equal(t, http.StatusBadRequest, code, body)

	code, _, body = f.post(t, testOwnerPrn, f.offline, `{"cmd":"LIST_CONTAINERS"}`)
	assert.Equal(t, http.StatusConflict, code, body)

	code, _, body = f.post(t, testStrangerPrn, f.connected, `{"cmd":"LIST_CONTAINERS"}`)
	assert.Equal(t, http.StatusNotFound, code, body)

	code, _, body = f.post(t, testOwnerPrn, primitive.NewObjectID(), `{"cmd":"LIST_CONTAINERS"}`)
	assert.Equal(t, http.StatusNotFound, code, body)
}

func (f *commandFixture) list(t *testing.T, caller string, device primitive.ObjectID, query string) (int, []DeviceCommandView) {
	t.Helper()
	rec := testRequest(t, f.app.handleGetCommands, caller, http.MethodGet, "/devices/"+device.Hex()+"/commands"+query,
		nil, echo.PathValue{Name: "id", Value: device.Hex()})
	views := []DeviceCommandView{}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &views))
	}
	return rec.Code, views
}

func TestGetCommands(t *testing.T) {
	f := newCommandFixture(t)
	commands := f.app.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection)

	// Three commands a minute apart; the oldest is past the timeout, one has
	// finished.
	base := time.Now().UTC().Truncate(time.Second).Add(-3 * time.Minute)
	ids := make([]primitive.ObjectID, 3)
	for i := range ids {
		ids[i] = primitive.NewObjectID()
		created := base.Add(time.Duration(i) * time.Minute)
		command := DeviceCommand{
			ID: ids[i], DeviceID: f.connected, Owner: testOwnerPrn, CreatedBy: testOwnerPrn,
			Cmd: "LIST_GROUPS", Args: map[string]interface{}{}, Status: CommandStatusPending,
			CreatedAt: created, ExpiresAt: created.Add(CommandExpiry),
		}
		_, err := commands.InsertOne(context.Background(), command)
		require.NoError(t, err)
	}
	_, err := commands.UpdateOne(context.Background(), bson.M{"_id": ids[1]}, bson.M{"$set": bson.M{
		"status": "ok", "code": 200, "output": bson.A{bson.M{"name": "root"}}, "finished_at": time.Now(),
	}})
	require.NoError(t, err)

	code, views := f.list(t, testOwnerPrn, f.connected, "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, views, 3)
	assert.Equal(t, []string{ids[2].Hex(), ids[1].Hex(), ids[0].Hex()}, []string{views[0].ID, views[1].ID, views[2].ID}, "newest first")
	assert.Equal(t, "pending", views[0].Status)
	assert.Equal(t, "ok", views[1].Status)
	assert.Equal(t, []interface{}{map[string]interface{}{"name": "root"}}, views[1].Output)
	assert.Equal(t, "timeout", views[2].Status, "pending past the timeout")

	code, views = f.list(t, testOwnerPrn, f.connected, "?limit=2")
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, views, 2)

	code, _ = f.list(t, testOwnerPrn, f.connected, "?limit=0")
	assert.Equal(t, http.StatusBadRequest, code)

	code, views = f.list(t, testOwnerPrn, f.offline, "")
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, views)

	code, _ = f.list(t, testStrangerPrn, f.connected, "")
	assert.Equal(t, http.StatusNotFound, code)

	// One command.
	get := func(caller string, device primitive.ObjectID, cid string) (int, DeviceCommandView) {
		rec := testRequest(t, f.app.handleGetCommand, caller, http.MethodGet, "/devices/"+device.Hex()+"/commands/"+cid, nil,
			echo.PathValue{Name: "id", Value: device.Hex()}, echo.PathValue{Name: "cid", Value: cid})
		view := DeviceCommandView{}
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		}
		return rec.Code, view
	}

	code, view := get(testOwnerPrn, f.connected, ids[0].Hex())
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "timeout", view.Status)

	code, view = get(testOwnerPrn, f.connected, ids[1].Hex())
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 200, view.Code)
	assert.NotNil(t, view.FinishedAt)

	code, _ = get(testOwnerPrn, f.offline, ids[0].Hex())
	assert.Equal(t, http.StatusNotFound, code, "a command is only found under its device")

	code, _ = get(testStrangerPrn, f.connected, ids[0].Hex())
	assert.Equal(t, http.StatusNotFound, code)

	code, _ = get(testOwnerPrn, f.connected, primitive.NewObjectID().Hex())
	assert.Equal(t, http.StatusNotFound, code)

	code, _ = get(testOwnerPrn, f.connected, "nothex")
	assert.Equal(t, http.StatusBadRequest, code)

}
