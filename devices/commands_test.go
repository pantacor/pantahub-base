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
	"sync"
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

func TestMqttBrokerReadsTheQuotedKeys(t *testing.T) {
	broker, ok := mqttBroker(map[string]interface{}{"pantahubＮmqttＮconnected": true, "pantahubＮmqttＮbroker": "b1"})
	assert.True(t, ok)
	assert.Equal(t, "b1", broker)

	for _, meta := range []map[string]interface{}{
		{"pantahubＮmqttＮconnected": false, "pantahubＮmqttＮbroker": "b1"},
		{"pantahubＮmqttＮconnected": "true", "pantahubＮmqttＮbroker": "b1"},
		{"pantahubＮmqttＮconnected": true},
		{"pantahubＮonline": true},
		nil,
	} {
		_, ok := mqttBroker(meta)
		assert.False(t, ok, "%v", meta)
	}
}

// Devices never write the broker's connection keys, however they spell them.
func TestStripMqttDeviceMeta(t *testing.T) {
	meta := map[string]interface{}{
		"pantahub.mqtt.connected":  true,
		"pantahubＮmqttＮbroker":     "forged",
		"pantahub.mqtt.connection": "x",
		"pantahub.online":          true,
		"pantavisor.sdk.mode":      "mqtt",
	}
	quoted := StripMqttDeviceMeta(utils.BsonQuoteMap(&meta))
	assert.Equal(t, map[string]interface{}{"pantahubＮonline": true, "pantavisorＮsdkＮmode": "mqtt"}, quoted)
}

type commandFixture struct {
	app       *App
	connected primitive.ObjectID
	offline   primitive.ObjectID
	// orphaned is recorded as connected to a broker replica whose heartbeat
	// stopped: killed before it could record its disconnects.
	orphaned primitive.ObjectID
}

// newCommandFixture stores three devices of testOwnerPrn: one the MQTT broker
// recorded as connected, one it recorded as disconnected and one recorded as
// connected to a dead replica.
func newCommandFixture(t *testing.T) *commandFixture {
	t.Helper()
	client := newTestClient(t)
	f := &commandFixture{
		app:       &App{mongoClient: client},
		connected: primitive.NewObjectID(), offline: primitive.NewObjectID(), orphaned: primitive.NewObjectID(),
	}
	ctx := context.Background()

	brokers := client.Database(utils.MongoDb).Collection(MqttBrokersCollection)
	_, err := brokers.InsertMany(ctx, []interface{}{
		bson.M{"_id": "live", "heartbeat_at": time.Now()},
		bson.M{"_id": "dead", "heartbeat_at": time.Now().Add(-MqttBrokerLease - time.Second)},
	})
	require.NoError(t, err)

	devices := client.Database(utils.MongoDb).Collection("pantahub_devices")
	for id, state := range map[primitive.ObjectID]struct {
		connected bool
		broker    string
	}{f.connected: {true, "live"}, f.offline: {false, "live"}, f.orphaned: {true, "dead"}} {
		meta := map[string]interface{}{
			DeviceMetaMqttConnected:  state.connected,
			DeviceMetaMqttBroker:     state.broker,
			DeviceMetaMqttConnection: state.broker + "/" + id.Hex(),
		}
		_, err := devices.InsertOne(ctx, bson.M{
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

	code, _, body = f.post(t, testOwnerPrn, f.orphaned, `{"cmd":"LIST_CONTAINERS"}`)
	assert.Equal(t, http.StatusConflict, code, "connected to a replica without heartbeat: %s", body)

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

func TestPostCommandScopes(t *testing.T) {
	scopes := utils.MarshalScopes(PostCommandScopes)
	for scope, want := range map[string]bool{
		utils.Scopes.API.String():            true,
		utils.Scopes.DeviceCommands.String(): true,
		// Device write access alone does not reach the device itself.
		utils.Scopes.Devices.String():      false,
		utils.Scopes.WriteDevices.String(): false,
		utils.Scopes.APIReadOnly.String():  false,
	} {
		assert.Equal(t, want, utils.MatchScope(scopes, []string{scope}), scope)
	}
}

func TestPostCommandRateLimit(t *testing.T) {
	f := newCommandFixture(t)

	code, _, body := f.post(t, testOwnerPrn, f.connected, `{"cmd":"REBOOT_DEVICE"}`)
	require.Equal(t, http.StatusCreated, code, body)
	code, _, body = f.post(t, testOwnerPrn, f.connected, `{"cmd":"REBOOT_DEVICE"}`)
	assert.Equal(t, http.StatusTooManyRequests, code, body)
	assert.Contains(t, body, "too many commands", "the reason reaches the caller")

	for i := 0; i < commandRateLimit-1; i++ {
		code, _, body = f.post(t, testOwnerPrn, f.connected, `{"cmd":"LIST_CONTAINERS"}`)
		require.Equal(t, http.StatusCreated, code, body)
	}
	code, _, body = f.post(t, testOwnerPrn, f.connected, `{"cmd":"LIST_CONTAINERS"}`)
	assert.Equal(t, http.StatusTooManyRequests, code, body)

	// Older commands no longer count.
	old := time.Now().Add(-2 * commandRateWindow)
	sent := bson.A{}
	for i := 0; i < commandRateLimit; i++ {
		sent = append(sent, old)
	}
	_, err := f.app.mongoClient.Database(utils.MongoDb).Collection(CommandLimitsCollection).UpdateOne(context.Background(),
		bson.M{"_id": f.connected}, bson.M{"$set": bson.M{"sent": sent, "reboot_at": old}})
	require.NoError(t, err)
	code, _, body = f.post(t, testOwnerPrn, f.connected, `{"cmd":"REBOOT_DEVICE"}`)
	assert.Equal(t, http.StatusCreated, code, body)
}

// Requests racing each other, as a double click or a retrying script sends
// them, cannot exceed the limits together.
func TestPostCommandRateLimitIsAtomic(t *testing.T) {
	f := newCommandFixture(t)

	race := func(n int, body string) (created, limited int) {
		codes := make(chan int, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, _, _ := f.post(t, testOwnerPrn, f.connected, body)
				codes <- code
			}()
		}
		wg.Wait()
		close(codes)
		for code := range codes {
			switch code {
			case http.StatusCreated:
				created++
			case http.StatusTooManyRequests:
				limited++
			default:
				t.Errorf("unexpected status %d", code)
			}
		}
		return created, limited
	}

	created, limited := race(5, `{"cmd":"REBOOT_DEVICE"}`)
	assert.Equal(t, 1, created, "reboots sent at once")
	assert.Equal(t, 4, limited)

	created, limited = race(3*commandRateLimit, `{"cmd":"LIST_CONTAINERS"}`)
	assert.Equal(t, commandRateLimit-1, created, "commands sent at once, after the reboot")
	assert.Equal(t, 3*commandRateLimit-(commandRateLimit-1), limited)

	stored, err := f.app.mongoClient.Database(utils.MongoDb).Collection(CommandsCollection).
		CountDocuments(context.Background(), bson.M{"device_id": f.connected})
	require.NoError(t, err)
	assert.Equal(t, int64(commandRateLimit), stored)
}

// The history of a device with a long past reads only the page it returns:
// no in-memory sort over every command the device was ever sent.
func TestCommandHistoryUsesItsIndex(t *testing.T) {
	f := newCommandFixture(t)
	require.NoError(t, f.app.EnsureCommandIndices())
	// Idempotent, including dropping the superseded index.
	require.NoError(t, f.app.EnsureCommandIndices())

	db := f.app.mongoClient.Database(utils.MongoDb)
	base := time.Now().UTC().Add(-time.Hour)
	docs := []interface{}{}
	for i := 0; i < 200; i++ {
		created := base.Add(time.Duration(i) * time.Second)
		docs = append(docs, DeviceCommand{
			ID: primitive.NewObjectID(), DeviceID: f.connected, Owner: testOwnerPrn, CreatedBy: testOwnerPrn,
			Cmd: "LIST_GROUPS", Args: map[string]interface{}{}, Status: CommandStatusOK,
			CreatedAt: created, ExpiresAt: created.Add(CommandExpiry),
		})
	}
	_, err := db.Collection(CommandsCollection).InsertMany(context.Background(), docs)
	require.NoError(t, err)

	filter, opts := commandHistoryQuery(f.connected, testOwnerPrn, commandsDefaultLimit)
	explain := bson.M{}
	err = db.RunCommand(context.Background(), bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: CommandsCollection},
			{Key: "filter", Value: filter},
			{Key: "sort", Value: opts.Sort},
			{Key: "limit", Value: *opts.Limit},
		}},
		{Key: "verbosity", Value: "executionStats"},
	}).Decode(&explain)
	require.NoError(t, err)

	stats, _ := explain["executionStats"].(bson.M)
	require.NotNil(t, stats, "%v", explain)
	assert.EqualValues(t, commandsDefaultLimit, stats["totalDocsExamined"], "documents read for one page")

	indexes, err := db.Collection(CommandsCollection).Indexes().ListSpecifications(context.Background())
	require.NoError(t, err)
	names := map[string]*int32{}
	for _, index := range indexes {
		names[index.Name] = index.ExpireAfterSeconds
	}
	assert.NotContains(t, names, "device_id_1_created_at_-1", "superseded index dropped")
	require.Contains(t, names, "created_at_1")
	require.NotNil(t, names["created_at_1"], "retention is a TTL index")
	assert.EqualValues(t, CommandRetention.Seconds(), *names["created_at_1"])
}

func TestDeleteDeviceCommands(t *testing.T) {
	f := newCommandFixture(t)

	code, _, body := f.post(t, testOwnerPrn, f.connected, `{"cmd":"LIST_CONTAINERS"}`)
	require.Equal(t, http.StatusCreated, code, body)
	other := DeviceCommand{ID: primitive.NewObjectID(), DeviceID: f.offline, Owner: testOwnerPrn, Status: CommandStatusOK, CreatedAt: time.Now()}
	db := f.app.mongoClient.Database(utils.MongoDb)
	_, err := db.Collection(CommandsCollection).InsertOne(context.Background(), other)
	require.NoError(t, err)

	require.NoError(t, f.app.DeleteDeviceCommands(context.Background(), f.connected))

	left, err := db.Collection(CommandsCollection).CountDocuments(context.Background(), bson.M{"device_id": f.connected})
	require.NoError(t, err)
	assert.Zero(t, left)
	limits, err := db.Collection(CommandLimitsCollection).CountDocuments(context.Background(), bson.M{"_id": f.connected})
	require.NoError(t, err)
	assert.Zero(t, limits)
	others, err := db.Collection(CommandsCollection).CountDocuments(context.Background(), bson.M{"device_id": f.offline})
	require.NoError(t, err)
	assert.EqualValues(t, 1, others, "other devices keep theirs")
}
