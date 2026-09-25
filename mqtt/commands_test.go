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

package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// envTestMongo points the tests that need a database at a disposable MongoDB.
// The change stream test additionally needs it to be a replica set.
const envTestMongo = "PANTAHUB_MQTT_TEST_MONGO"

func TestStatusMeta(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    bool
	}{
		{`{"online": true, "status": "online"}`, true},
		{`{"online": false, "status": "offline"}`, false},
		{`"offline"`, false},
		{`online`, true},
	} {
		meta := statusMeta([]byte(tc.payload))

		if got := meta["pantahub.online"]; got != tc.want {
			t.Errorf("%s: pantahub.online = %v, want %v", tc.payload, got, tc.want)
		}
		if _, ok := meta["pantahub.status"]; !ok {
			t.Errorf("%s: pantahub.status dropped", tc.payload)
		}
		// The connection keys are the broker's, never derived from what
		// the device says.
		for key := range meta {
			if strings.HasPrefix(key, devices.DeviceMetaMqttPrefix) {
				t.Errorf("%s: status meta writes %s", tc.payload, key)
			}
		}
	}
}

func TestDecodeCommandResult(t *testing.T) {
	id := primitive.NewObjectID()

	for _, tc := range []struct {
		name   string
		output string
		want   interface{}
	}{
		{"array", `[{"name":"os","status":"STARTED"}]`, []interface{}{map[string]interface{}{"name": "os", "status": "STARTED"}}},
		{"string", `"not json"`, "not json"},
		{"null", `null`, nil},
		{"object", `{"a":1}`, map[string]interface{}{"a": float64(1)}},
	} {
		payload := fmt.Sprintf(`{"id":%q,"cmd":"LIST_CONTAINERS","status":"ok","code":200,"output":%s,"error":""}`, id.Hex(), tc.output)
		result, err := decodeCommandResult([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if result.id != id || result.status != "ok" || result.code != 200 {
			t.Errorf("%s: decoded %+v", tc.name, result)
		}
		wantJSON, _ := json.Marshal(tc.want)
		gotJSON, _ := json.Marshal(result.output)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("%s: output = %s, want %s", tc.name, gotJSON, wantJSON)
		}
	}

	// output may be missing altogether.
	result, err := decodeCommandResult([]byte(fmt.Sprintf(`{"id":%q,"status":"error","code":0,"error":"pv-ctrl unreachable"}`, id.Hex())))
	if err != nil || result.output != nil || result.error != "pv-ctrl unreachable" {
		t.Errorf("missing output: result %+v, err %v", result, err)
	}
}

func TestDecodeCommandResultRejectsMalformed(t *testing.T) {
	id := primitive.NewObjectID().Hex()

	for _, payload := range []string{
		``,
		`not json`,
		`[]`,
		`{"status":"ok"}`,
		`{"id":"nothex","status":"ok"}`,
		`{"id":"` + id + `"}`,
		`{"id":"` + id + `","status":"pending"}`,
		`{"id":"` + id + `","status":"timeout"}`,
		`{"id":"` + id + `","status":"OK"}`,
		`{"id":"` + id + `","status":"ok","code":"200"}`,
	} {
		if result, err := decodeCommandResult([]byte(payload)); err == nil {
			t.Errorf("decodeCommandResult(%s) accepted: %+v", payload, result)
		}
	}
}

// A device may complete only its own commands, and only while pending.
func TestCommandResultUpdatePinsDeviceAndPending(t *testing.T) {
	device := primitive.NewObjectID()
	result := commandResult{id: primitive.NewObjectID(), status: "ok", code: 200, output: "x"}
	now := time.Now().UTC()

	filter, update := commandResultUpdate(device, result, now)

	if filter["_id"] != result.id || filter["device_id"] != device || filter["status"] != devices.CommandStatusPending || len(filter) != 3 {
		t.Errorf("filter = %v", filter)
	}
	set, _ := update["$set"].(bson.M)
	for key, want := range map[string]interface{}{
		"status": "ok", "code": 200, "output": "x", "error": "", "finished_at": now,
	} {
		if set[key] != want {
			t.Errorf("$set[%s] = %v, want %v", key, set[key], want)
		}
	}
}

// newTestBridge builds a bridge whose queue can be inspected: no workers run.
func newTestBridge() *bridgeHook {
	return &bridgeHook{jobs: make(chan bridgeJob, 4), done: make(chan struct{})}
}

// Results a device may not report are refused before anything is written.
func TestIngestCommandResultRefusals(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	topic := Topic(deviceID, SuffixCommandsResult)
	valid := []byte(`{"id":"` + primitive.NewObjectID().Hex() + `","cmd":"RUN_GC","status":"ok","code":200,"output":null,"error":""}`)

	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")

	// No mongo client: reaching a write would panic.
	h := newTestBridge()
	if err := h.ingest(device, topic, []byte(`{"id":"x","status":"ok"}`), nil); err == nil {
		t.Error("malformed result accepted")
	}

	other := &mochi.Client{}
	setIdentity(other, kindDevice, primitive.NewObjectID().Hex(), "")
	if err := h.ingest(other, topic, valid, nil); err == nil {
		t.Error("a device completed another device's command")
	}

	user := &mochi.Client{}
	setIdentity(user, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	if err := h.ingest(user, topic, valid, nil); err == nil {
		t.Error("a user session forged a command result")
	}
}

func TestDecodeCommandResultCapsError(t *testing.T) {
	payload := fmt.Sprintf(`{"id":%q,"status":"error","error":%q}`, primitive.NewObjectID().Hex(), strings.Repeat("é", maxCommandErrorLength))
	result, err := decodeCommandResult([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.error) > maxCommandErrorLength || !utf8.ValidString(result.error) {
		t.Errorf("error of %d bytes kept, valid UTF-8 %v", len(result.error), utf8.ValidString(result.error))
	}
}

func TestCommandMessage(t *testing.T) {
	now := time.Date(2026, 9, 25, 15, 9, 0, 0, time.UTC)
	command := devices.DeviceCommand{
		ID:        primitive.NewObjectID(),
		DeviceID:  primitive.NewObjectID(),
		Cmd:       "REBOOT_DEVICE",
		Args:      map[string]interface{}{"message": "bye"},
		Status:    devices.CommandStatusPending,
		CreatedAt: now,
		ExpiresAt: now.Add(devices.CommandExpiry),
	}

	topic, payload, ok := commandMessage(&command, now)
	if !ok {
		t.Fatal("pending command not sent")
	}
	if topic != "ph/v1/dev/"+command.DeviceID.Hex()+"/commands" {
		t.Errorf("topic = %q", topic)
	}
	want := `{"id":"` + command.ID.Hex() + `","cmd":"REBOOT_DEVICE","args":{"message":"bye"},"expires_at":"2026-09-25T15:10:00Z"}`
	if string(payload) != want {
		t.Errorf("payload = %s\nwant      %s", payload, want)
	}

	noArgs := command
	noArgs.Cmd, noArgs.Args = "LIST_CONTAINERS", nil
	if _, payload, _ := commandMessage(&noArgs, now); !strings.Contains(string(payload), `"args":{}`) {
		t.Errorf("args must be an empty object, payload = %s", payload)
	}

	for name, mutate := range map[string]func(*devices.DeviceCommand){
		"expired":  func(c *devices.DeviceCommand) { c.ExpiresAt = now },
		"finished": func(c *devices.DeviceCommand) { c.Status = devices.CommandStatusOK },
		"unknown":  func(c *devices.DeviceCommand) { c.Cmd = "RUN_SHELL" },
		"nodevice": func(c *devices.DeviceCommand) { c.DeviceID = primitive.NilObjectID },
	} {
		c := command
		mutate(&c)
		if _, _, ok := commandMessage(&c, now); ok {
			t.Errorf("%s command was sent", name)
		}
	}
}

// newTestMongo connects to the disposable MongoDB and points the package at a
// fresh database that is dropped afterwards.
func newTestMongo(t *testing.T) *mongo.Client {
	t.Helper()
	uri := os.Getenv(envTestMongo)
	if uri == "" {
		t.Skip(envTestMongo + " is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}

	previousDb := utils.MongoDb
	utils.MongoDb = fmt.Sprintf("mqtt_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		client.Database(utils.MongoDb).Drop(context.Background())
		utils.MongoDb = previousDb
		client.Disconnect(context.Background())
	})
	return client
}

func insertPendingCommand(t *testing.T, client *mongo.Client, deviceID primitive.ObjectID) primitive.ObjectID {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	command := devices.DeviceCommand{
		ID: primitive.NewObjectID(), DeviceID: deviceID, Owner: "prn:pantahub.com:auth:/alice",
		CreatedBy: "prn:pantahub.com:auth:/alice", Cmd: "LIST_CONTAINERS", Args: map[string]interface{}{},
		Status: devices.CommandStatusPending, CreatedAt: now, ExpiresAt: now.Add(devices.CommandExpiry),
	}
	if _, err := client.Database(utils.MongoDb).Collection(devices.CommandsCollection).InsertOne(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	return command.ID
}

func loadCommand(t *testing.T, client *mongo.Client, id primitive.ObjectID) devices.DeviceCommand {
	t.Helper()
	command := devices.DeviceCommand{}
	err := client.Database(utils.MongoDb).Collection(devices.CommandsCollection).
		FindOne(context.Background(), bson.M{"_id": id}).Decode(&command)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func TestPutCommandResult(t *testing.T) {
	client := newTestMongo(t)
	h := &bridgeHook{mongoClient: client}
	ctx := context.Background()

	device := primitive.NewObjectID()
	commandID := insertPendingCommand(t, client, device)

	payload := []byte(`{"id":"` + commandID.Hex() + `","cmd":"LIST_CONTAINERS","status":"ok","code":200,` +
		`"output":[{"name":"os","status":"STARTED","lo.ipv4":"127.0.0.1","$where":"x"}],"error":""}`)
	result, err := decodeCommandResult(payload)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.putCommandResult(ctx, primitive.NewObjectID(), result); err == nil {
		t.Fatal("another device completed the command")
	}
	if got := loadCommand(t, client, commandID); got.Status != devices.CommandStatusPending {
		t.Fatalf("status = %q after a foreign result", got.Status)
	}

	if err := h.putCommandResult(ctx, device, result); err != nil {
		t.Fatal(err)
	}
	got := loadCommand(t, client, commandID)
	if got.Status != "ok" || got.Code != 200 || got.FinishedAt == nil {
		t.Fatalf("stored %+v", got)
	}
	outputJSON, _ := json.Marshal(devices.DecodeCommandOutput(got.Output))
	if want := `[{"$where":"x","lo.ipv4":"127.0.0.1","name":"os","status":"STARTED"}]`; string(outputJSON) != want {
		t.Errorf("output = %s, want %s", outputJSON, want)
	}

	// A second answer (or a QoS 1 redelivery) matches nothing.
	result.status = "error"
	if err := h.putCommandResult(ctx, device, result); err == nil {
		t.Error("a finished command was completed again")
	}
	if got := loadCommand(t, client, commandID); got.Status != "ok" {
		t.Errorf("status = %q after a second result", got.Status)
	}
}

// TestNotifierDeliversCommandInserts runs the real change stream: a command
// inserted into the collection reaches a subscriber of the device's commands
// topic, not retained.
func TestNotifierDeliversCommandInserts(t *testing.T) {
	client := newTestMongo(t)

	server := mochi.New(&mochi.Options{InlineClient: true})
	if err := server.Serve(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	device := primitive.NewObjectID()
	received := make(chan packets.Packet, 4)
	err := server.Subscribe(Topic(device.Hex(), SuffixCommands), 1, func(_ *mochi.Client, _ packets.Subscription, pk packets.Packet) {
		received <- pk
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifier := NewNotifier(client, server)
	go notifier.Run(ctx)

	// The stream opens asynchronously; insert until one is delivered, so the
	// test does not depend on how long that takes.
	deadline := time.After(20 * time.Second)
	var pk packets.Packet
	for pk.TopicName == "" {
		insertPendingCommand(t, client, device)
		select {
		case pk = <-received:
		case <-time.After(time.Second):
		case <-deadline:
			t.Fatal("no command delivered")
		}
	}

	notice := commandNotice{}
	if err := json.Unmarshal(pk.Payload, &notice); err != nil {
		t.Fatalf("payload %s: %v", pk.Payload, err)
	}
	if notice.Cmd != "LIST_CONTAINERS" || notice.Args == nil || notice.ExpiresAt == "" {
		t.Errorf("payload = %s", pk.Payload)
	}
	if _, err := primitive.ObjectIDFromHex(notice.ID); err != nil {
		t.Errorf("id = %q", notice.ID)
	}
	if pk.FixedHeader.Retain {
		t.Error("command published retained")
	}
	if pk.Properties.MessageExpiryInterval == 0 || pk.Properties.MessageExpiryInterval > uint32(devices.CommandExpiry.Seconds()) || pk.Expiry == 0 {
		t.Errorf("command expiry interval %d, expiry %d", pk.Properties.MessageExpiryInterval, pk.Expiry)
	}
	if _, retained := server.Topics.Retained.Get(pk.TopicName); retained {
		t.Error("broker retained a command")
	}
}

func TestCommandExpiryInterval(t *testing.T) {
	now := time.Now()
	for left, want := range map[time.Duration]uint32{
		60 * time.Second:       60,
		59*time.Second + 1:     60,
		500 * time.Millisecond: 1,
		0:                      1,
		-10 * time.Second:      1,
	} {
		if got := commandExpiryInterval(now.Add(left), now); got != want {
			t.Errorf("%v left: expiry %d, want %d", left, got, want)
		}
	}
}

// A command queued in the persistent session of a device that is not
// connected to this replica is dropped by the broker once it expires, instead
// of being replayed when the device reconnects here.
func TestQueuedCommandExpires(t *testing.T) {
	b := newTestBrokerReplica(t, nil, "A")
	deviceID := primitive.NewObjectID().Hex()

	conn := b.dialDevice(t, deviceID, true)
	conn.Close()
	b.waitDisconnect(t)

	notifier := NewNotifier(nil, b.server)
	notifier.publishCommand(Topic(deviceID, SuffixCommands), []byte(`{}`), 5)

	session, ok := b.server.Clients.Get(deviceID)
	if !ok {
		t.Fatal("persistent session not kept")
	}
	if session.State.Inflight.Len() != 1 {
		t.Fatalf("%d commands queued, want 1", session.State.Inflight.Len())
	}
	if dropped := session.ClearExpiredInflights(time.Now().Unix()+1, 0); len(dropped) != 0 {
		t.Fatal("command dropped before it expired")
	}
	if dropped := session.ClearExpiredInflights(time.Now().Unix()+6, 0); len(dropped) != 1 {
		t.Fatal("expired command still queued")
	}
}

// A result is recorded before the broker acknowledges it: once ingest
// returns, the command is complete.
func TestIngestRecordsCommandResult(t *testing.T) {
	client := newTestMongo(t)
	h := &bridgeHook{mongoClient: client}
	device := primitive.NewObjectID()
	topic := Topic(device.Hex(), SuffixCommandsResult)
	cl := &mochi.Client{}
	setIdentity(cl, kindDevice, device.Hex(), "")

	commandID := insertPendingCommand(t, client, device)
	payload := []byte(`{"id":"` + commandID.Hex() + `","cmd":"LIST_CONTAINERS","status":"ok","code":200,"output":[{"id":9007199254740993}],"error":""}`)
	if err := h.ingest(cl, topic, payload, nil); err != nil {
		t.Fatal(err)
	}
	got := loadCommand(t, client, commandID)
	if got.Status != "ok" {
		t.Fatalf("status = %q right after ingest", got.Status)
	}
	outputJSON, _ := json.Marshal(devices.DecodeCommandOutput(got.Output))
	if string(outputJSON) != `[{"id":9007199254740993}]` {
		t.Errorf("output = %s", outputJSON)
	}

	// A redelivery completes nothing, and is still acknowledged.
	if err := h.ingest(cl, topic, payload, nil); err != nil {
		t.Errorf("redelivered result refused: %v", err)
	}
}

// Output the database cannot store does not leave the command pending: it is
// stored as text, with the reason.
func TestUnstorableOutputIsStoredAsText(t *testing.T) {
	client := newTestMongo(t)
	h := &bridgeHook{mongoClient: client}
	device := primitive.NewObjectID()
	commandID := insertPendingCommand(t, client, device)

	deep := strings.Repeat(`{"a":`, 300) + `1` + strings.Repeat(`}`, 300)
	payload := []byte(`{"id":"` + commandID.Hex() + `","status":"ok","code":200,"output":` + deep + `,"error":""}`)
	result, err := decodeCommandResult(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.putCommandResult(context.Background(), device, result); err != nil {
		t.Fatal(err)
	}

	got := loadCommand(t, client, commandID)
	if got.Status != "ok" || got.FinishedAt == nil {
		t.Fatalf("stored %+v", got)
	}
	if text, _ := devices.DecodeCommandOutput(got.Output).(string); text != deep {
		t.Errorf("output not kept as text: %v", devices.DecodeCommandOutput(got.Output))
	}
	if !strings.Contains(got.Error, "output stored as text") {
		t.Errorf("error = %q", got.Error)
	}
}

// When the database cannot be reached the result is refused, so the broker
// does not acknowledge it and the device sends it again.
func TestUnrecordedResultIsNotAcknowledged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://127.0.0.1:1/?connectTimeoutMS=200&serverSelectionTimeoutMS=200"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	previousDb := utils.MongoDb
	utils.MongoDb = "unreachable"
	defer func() { utils.MongoDb = previousDb }()

	h := &bridgeHook{mongoClient: client}
	device := primitive.NewObjectID()
	cl := &mochi.Client{}
	setIdentity(cl, kindDevice, device.Hex(), "")
	payload := []byte(`{"id":"` + primitive.NewObjectID().Hex() + `","status":"ok","code":200,"output":null,"error":""}`)

	if err := h.ingest(cl, Topic(device.Hex(), SuffixCommandsResult), payload, nil); err == nil {
		t.Fatal("a result that could not be recorded was accepted")
	}
}

// A device may retain its liveness and nothing else it reports.
func TestDeviceRetainsOnlyStatus(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")
	h := newTestBridge()

	publish := func(suffix string, payload string) error {
		pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Retain: true}, TopicName: Topic(deviceID, suffix), Payload: []byte(payload)}
		_, err := h.OnPublish(device, pk)
		return err
	}

	if err := publish(SuffixStatus, `{"online":true}`); err != nil {
		t.Errorf("retained status refused: %v", err)
	}
	for _, suffix := range []string{SuffixCommandsResult, SuffixDeviceMeta, SuffixLogs, SuffixUserMetaGet} {
		if err := publish(suffix, `{}`); !errors.Is(err, packets.ErrRejectPacket) {
			t.Errorf("retained %s accepted", suffix)
		}
	}

	inline := &mochi.Client{}
	inline.Net.Inline = true
	if !mayRetain(inline, Topic(deviceID, SuffixStepsNew)) {
		t.Error("the Hub's own retained publish refused")
	}
}
