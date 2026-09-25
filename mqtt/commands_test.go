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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

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

func TestStatusMetaRecordsMqttConnection(t *testing.T) {
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

		if got := meta["pantahub.mqtt.connected"]; got != tc.want {
			t.Errorf("%s: pantahub.mqtt.connected = %v, want %v", tc.payload, got, tc.want)
		}
		if got := meta["pantahub.online"]; got != tc.want {
			t.Errorf("%s: pantahub.online = %v, want %v", tc.payload, got, tc.want)
		}
		if _, ok := meta["pantahub.status"]; !ok {
			t.Errorf("%s: pantahub.status dropped", tc.payload)
		}

		statusTime, _ := meta["pantahub.mqtt.status-time"].(string)
		parsed, err := time.Parse(time.RFC3339, statusTime)
		if err != nil || !strings.HasSuffix(statusTime, "Z") {
			t.Errorf("%s: pantahub.mqtt.status-time = %q, want RFC 3339 UTC", tc.payload, statusTime)
		} else if time.Since(parsed) > time.Minute {
			t.Errorf("%s: pantahub.mqtt.status-time = %q is not now", tc.payload, statusTime)
		}
	}
}

// The key the bridge writes must be the key the command endpoint reads.
func TestStatusMetaKeyMatchesCommandGate(t *testing.T) {
	meta := statusMeta([]byte(`{"online": true}`))
	quoted := utils.BsonQuoteMap(&meta)
	if quoted[utils.BsonQuote(devices.DeviceMetaMqttConnected)] != true {
		t.Fatalf("quoted status meta %v does not carry %s", quoted, devices.DeviceMetaMqttConnected)
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

func TestIngestCommandResult(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	topic := Topic(deviceID, SuffixCommandsResult)
	valid := []byte(`{"id":"` + primitive.NewObjectID().Hex() + `","cmd":"RUN_GC","status":"ok","code":200,"output":null,"error":""}`)

	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")

	h := newTestBridge()
	if err := h.ingest(device, topic, valid); err != nil {
		t.Fatalf("valid result refused: %v", err)
	}
	if len(h.jobs) != 1 {
		t.Fatalf("valid result queued %d writes, want 1", len(h.jobs))
	}

	h = newTestBridge()
	if err := h.ingest(device, topic, []byte(`{"id":"x","status":"ok"}`)); err == nil {
		t.Error("malformed result accepted")
	}

	other := &mochi.Client{}
	setIdentity(other, kindDevice, primitive.NewObjectID().Hex(), "")
	if err := h.ingest(other, topic, valid); err == nil {
		t.Error("a device completed another device's command")
	}

	user := &mochi.Client{}
	setIdentity(user, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	if err := h.ingest(user, topic, valid); err == nil {
		t.Error("a user session forged a command result")
	}

	if len(h.jobs) != 0 {
		t.Errorf("refused results queued %d writes", len(h.jobs))
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
	if _, retained := server.Topics.Retained.Get(pk.TopicName); retained {
		t.Error("broker retained a command")
	}
}

// A will from a session taken over by a reconnect of the same device must not
// mark the (now connected) device offline.
func TestBridgeIgnoresWillOfTakenOverSession(t *testing.T) {
	server := mochi.New(&mochi.Options{InlineClient: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := server.AddHook(new(allowAll), nil); err != nil {
		t.Fatal(err)
	}
	h := newTestBridge()
	deviceID := primitive.NewObjectID().Hex()
	wills := make(chan bool, 4)
	if err := server.AddHook(&willRecorder{onWill: func(cl *mochi.Client) {
		setIdentity(cl, kindDevice, deviceID, "")
		h.OnWillSent(cl, packets.Packet{TopicName: Topic(deviceID, SuffixStatus), Payload: []byte(`{"online":false,"status":"offline"}`)})
		wills <- cl.IsTakenOver()
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	connect := func() net.Conn {
		srv, cli := net.Pipe()
		go server.EstablishConnection("test", srv)
		pk := packets.Packet{
			FixedHeader:     packets.FixedHeader{Type: packets.Connect},
			ProtocolVersion: 4,
			Connect: packets.ConnectParams{
				ProtocolName:     []byte("MQTT"),
				Clean:            false,
				Keepalive:        60,
				ClientIdentifier: deviceID,
				WillFlag:         true,
				WillTopic:        Topic(deviceID, SuffixStatus),
				WillPayload:      []byte(`{"online":false,"status":"offline"}`),
				WillRetain:       true,
			},
		}
		var buf bytes.Buffer
		if err := pk.ConnectEncode(&buf); err != nil {
			t.Fatal(err)
		}
		go cli.Write(buf.Bytes())
		ack := make([]byte, 4)
		if _, err := io.ReadFull(cli, ack); err != nil {
			t.Fatal(err)
		}
		go io.Copy(io.Discard, cli)
		return cli
	}

	old := connect()
	defer old.Close()
	fresh := connect() // the device again: takes the session over
	defer fresh.Close()

	select {
	case takenOver := <-wills:
		if !takenOver {
			t.Fatal("will came from a session that was not taken over")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broker did not send the taken-over session's will")
	}
	select {
	case job := <-h.jobs:
		t.Fatalf("taken-over will was recorded: %s", job.topic)
	default:
	}
}

type allowAll struct{ mochi.HookBase }

func (h *allowAll) ID() string { return "allow-all" }
func (h *allowAll) Provides(b byte) bool {
	return b == mochi.OnConnectAuthenticate || b == mochi.OnACLCheck
}
func (h *allowAll) OnConnectAuthenticate(*mochi.Client, packets.Packet) bool { return true }
func (h *allowAll) OnACLCheck(*mochi.Client, string, bool) bool              { return true }

type willRecorder struct {
	mochi.HookBase
	onWill func(*mochi.Client)
}

func (h *willRecorder) ID() string                                    { return "will-recorder" }
func (h *willRecorder) Provides(b byte) bool                          { return b == mochi.OnWillSent }
func (h *willRecorder) OnWillSent(cl *mochi.Client, _ packets.Packet) { h.onWill(cl) }
