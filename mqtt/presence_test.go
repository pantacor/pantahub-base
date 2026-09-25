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
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// deviceIdentityHook authenticates every client as the device its client id
// names, and allows everything.
type deviceIdentityHook struct{ mochi.HookBase }

func (h *deviceIdentityHook) ID() string { return "device-identity" }
func (h *deviceIdentityHook) Provides(b byte) bool {
	return b == mochi.OnConnectAuthenticate || b == mochi.OnACLCheck
}
func (h *deviceIdentityHook) OnConnectAuthenticate(cl *mochi.Client, pk packets.Packet) bool {
	setIdentity(cl, kindDevice, string(pk.Connect.ClientIdentifier), "")
	return true
}
func (h *deviceIdentityHook) OnACLCheck(*mochi.Client, string, bool) bool { return true }

// eventRecorder reports wills sent and disconnects, in the order the broker
// runs them. It is added last, so it sees each event after the hooks under
// test did.
type eventRecorder struct {
	mochi.HookBase
	wills       chan *mochi.Client
	disconnects chan *mochi.Client
}

func newEventRecorder() *eventRecorder {
	return &eventRecorder{wills: make(chan *mochi.Client, 8), disconnects: make(chan *mochi.Client, 8)}
}

func (h *eventRecorder) ID() string { return "event-recorder" }
func (h *eventRecorder) Provides(b byte) bool {
	return b == mochi.OnWillSent || b == mochi.OnDisconnect
}
func (h *eventRecorder) OnWillSent(cl *mochi.Client, _ packets.Packet) { h.wills <- cl }
func (h *eventRecorder) OnDisconnect(cl *mochi.Client, _ error, _ bool) {
	h.disconnects <- cl
}

// uninitialisedBridge registers a bridge without starting its workers, so the
// test runs the queued writes itself.
type uninitialisedBridge struct{ *bridgeHook }

func (uninitialisedBridge) Init(any) error { return nil }
func (uninitialisedBridge) Stop() error    { return nil }

type testBroker struct {
	server   *mochi.Server
	presence *presenceHook
	bridge   *bridgeHook
	events   *eventRecorder
}

// newTestBrokerReplica runs one broker replica with the production presence
// hook and a bridge whose queue the test drains itself.
func newTestBrokerReplica(t *testing.T, client *mongo.Client, brokerID string) *testBroker {
	t.Helper()
	b := &testBroker{
		server: mochi.New(&mochi.Options{InlineClient: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}),
		bridge: newTestBridge(),
		events: newEventRecorder(),
	}
	b.bridge.mongoClient = client
	b.presence = &presenceHook{mongoClient: client, server: b.server, brokerID: brokerID}

	for _, hook := range []mochi.Hook{new(deviceIdentityHook), b.presence, uninitialisedBridge{b.bridge}, b.events} {
		if err := b.server.AddHook(hook, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.server.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.server.Close() })
	return b
}

// dialDevice connects deviceID with a persistent session and its offline
// will, and subscribes to its commands topic when subscribe is set. It
// returns once the broker has acknowledged both.
func (b *testBroker) dialDevice(t *testing.T, deviceID string, subscribe bool) net.Conn {
	t.Helper()
	srv, cli := net.Pipe()
	go b.server.EstablishConnection("test", srv)

	connect := packets.Packet{
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
	if err := connect.ConnectEncode(&buf); err != nil {
		t.Fatal(err)
	}
	go cli.Write(buf.Bytes())
	if _, err := io.ReadFull(cli, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	// The broker only starts reading once the session is established
	// (OnSessionEstablished), so the answer to a ping means it is.
	go cli.Write([]byte{packets.Pingreq << 4, 0})
	if _, err := io.ReadFull(cli, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}

	if subscribe {
		sub := packets.Packet{
			FixedHeader:     packets.FixedHeader{Type: packets.Subscribe, Qos: 1},
			ProtocolVersion: 4,
			PacketID:        1,
			Filters:         packets.Subscriptions{{Filter: Topic(deviceID, SuffixCommands), Qos: 1}},
		}
		buf.Reset()
		if err := sub.SubscribeEncode(&buf); err != nil {
			t.Fatal(err)
		}
		go cli.Write(buf.Bytes())
		// SUBACK: fixed header, packet id, one reason code.
		if _, err := io.ReadFull(cli, make([]byte, 5)); err != nil {
			t.Fatal(err)
		}
	}

	go io.Copy(io.Discard, cli)
	return cli
}

func (b *testBroker) waitDisconnect(t *testing.T) *mochi.Client {
	t.Helper()
	select {
	case cl := <-b.events.disconnects:
		return cl
	case <-time.After(5 * time.Second):
		t.Fatal("no disconnect")
		return nil
	}
}

// A device that reconnects before the broker noticed its old socket died
// takes its session over. The old session's will must not be sent: it would
// report the device offline right as it came back.
func TestTakeoverSendsNoWill(t *testing.T) {
	b := newTestBrokerReplica(t, nil, "A")
	deviceID := primitive.NewObjectID().Hex()

	old := b.dialDevice(t, deviceID, true)
	defer old.Close()
	fresh := b.dialDevice(t, deviceID, true) // takes the session over
	defer fresh.Close()

	// OnDisconnect runs after the will would have been sent, on the same
	// goroutine: once it is seen, the outcome is settled.
	if cl := b.waitDisconnect(t); !cl.IsTakenOver() {
		t.Fatal("the disconnect is not the taken-over session's")
	}
	select {
	case <-b.events.wills:
		t.Fatal("the taken-over session's will was sent")
	default:
	}
	if len(b.bridge.jobs) != 0 {
		t.Fatalf("%d writes queued for the taken-over session", len(b.bridge.jobs))
	}
	if _, retained := b.server.Topics.Retained.Get(Topic(deviceID, SuffixStatus)); retained {
		t.Error("the taken-over session's will was retained")
	}
}

// A connection that dies without a takeover still sends its will.
func TestDeadConnectionSendsWill(t *testing.T) {
	b := newTestBrokerReplica(t, nil, "A")
	deviceID := primitive.NewObjectID().Hex()

	conn := b.dialDevice(t, deviceID, true)
	conn.Close()
	b.waitDisconnect(t)

	select {
	case <-b.events.wills:
	default:
		t.Fatal("no will for a dead connection")
	}
}

func insertTestDevice(t *testing.T, client *mongo.Client) primitive.ObjectID {
	t.Helper()
	id := primitive.NewObjectID()
	_, err := client.Database(utils.MongoDb).Collection(devicesCollection).InsertOne(context.Background(), bson.M{
		"_id": id, "prn": "prn:::devices:/" + id.Hex(), "owner": "prn:pantahub.com:auth:/alice",
		"device-meta": bson.M{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func loadDeviceMeta(t *testing.T, client *mongo.Client, id primitive.ObjectID) map[string]interface{} {
	t.Helper()
	device := devices.Device{}
	err := client.Database(utils.MongoDb).Collection(devicesCollection).FindOne(context.Background(), bson.M{"_id": id}).Decode(&device)
	if err != nil {
		t.Fatal(err)
	}
	return utils.BsonUnquoteMap(&device.DeviceMeta)
}

// runQueued runs the device-meta writes the bridge queued, as its workers
// would.
func (b *testBroker) runQueued(t *testing.T) {
	t.Helper()
	for {
		select {
		case job := <-b.bridge.jobs:
			if err := job.write(context.Background()); err != nil {
				t.Fatalf("%s: %v", job.topic, err)
			}
		default:
			return
		}
	}
}

// Two replicas behind one ingress: the device reconnects to B while A still
// holds its dead socket. A's late disconnect and will must not mark it
// offline.
func TestPresenceAcrossReplicas(t *testing.T) {
	client := newTestMongo(t)
	a := newTestBrokerReplica(t, client, "A")
	b := newTestBrokerReplica(t, client, "B")
	device := insertTestDevice(t, client)

	// Connected, but not subscribed to commands yet: not reachable.
	onA := a.dialDevice(t, device.Hex(), false)
	if meta := loadDeviceMeta(t, client, device); meta[devices.DeviceMetaMqttConnected] != false {
		t.Fatalf("marked connected before subscribing to commands: %v", meta)
	}
	onA.Close()
	a.waitDisconnect(t)
	a.runQueued(t)

	onA = a.dialDevice(t, device.Hex(), true)
	meta := loadDeviceMeta(t, client, device)
	if meta[devices.DeviceMetaMqttConnected] != true || meta[devices.DeviceMetaMqttBroker] != "A" {
		t.Fatalf("after connecting to A: %v", meta)
	}
	connectionA, _ := meta[devices.DeviceMetaMqttConnection].(string)

	onB := b.dialDevice(t, device.Hex(), true)
	defer onB.Close()
	meta = loadDeviceMeta(t, client, device)
	if meta[devices.DeviceMetaMqttConnected] != true || meta[devices.DeviceMetaMqttBroker] != "B" || meta[devices.DeviceMetaMqttConnection] == connectionA {
		t.Fatalf("after reconnecting to B: %v", meta)
	}

	// The device reports itself online over B.
	if err := b.bridge.patchDeviceMeta(context.Background(), device, statusMeta([]byte(`{"online":true,"status":"online"}`)), nil); err != nil {
		t.Fatal(err)
	}

	// A notices its socket is dead: disconnect and will.
	onA.Close()
	a.waitDisconnect(t)
	a.runQueued(t)

	meta = loadDeviceMeta(t, client, device)
	if meta[devices.DeviceMetaMqttConnected] != true || meta[devices.DeviceMetaMqttBroker] != "B" {
		t.Fatalf("A's late disconnect marked the device offline: %v", meta)
	}
	if meta["pantahub.online"] != true {
		t.Fatalf("A's late will was recorded: %v", meta)
	}

	onB.Close()
	b.waitDisconnect(t)
	b.runQueued(t)
	meta = loadDeviceMeta(t, client, device)
	if meta[devices.DeviceMetaMqttConnected] != false {
		t.Fatalf("still connected after B's disconnect: %v", meta)
	}
	if meta["pantahub.online"] != false {
		t.Fatalf("B's will was not recorded: %v", meta)
	}
}

// A device that sets the broker's keys itself does not get them stored.
func TestDeviceCannotWriteConnectionKeys(t *testing.T) {
	client := newTestMongo(t)
	h := &bridgeHook{mongoClient: client}
	device := insertTestDevice(t, client)

	err := h.patchDeviceMeta(context.Background(), device, map[string]interface{}{
		"pantahub.mqtt.connected": true,
		"pantahub.mqtt.broker":    "forged",
		"pantavisor.sdk.mode":     "mqtt",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta := loadDeviceMeta(t, client, device)
	if meta[devices.DeviceMetaMqttConnected] != nil || meta[devices.DeviceMetaMqttBroker] != nil {
		t.Errorf("device wrote the connection keys: %v", meta)
	}
	if meta["pantavisor.sdk.mode"] != "mqtt" {
		t.Errorf("other keys dropped: %v", meta)
	}
}

func TestBrokerHeartbeat(t *testing.T) {
	client := newTestMongo(t)
	h := &presenceHook{mongoClient: client, brokerID: newBrokerID()}
	brokers := client.Database(utils.MongoDb).Collection(devices.MqttBrokersCollection)

	if err := h.beat(context.Background()); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		HeartbeatAt time.Time `bson:"heartbeat_at"`
	}
	if err := brokers.FindOne(context.Background(), bson.M{"_id": h.brokerID}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if time.Since(doc.HeartbeatAt) > time.Minute {
		t.Errorf("heartbeat_at = %v", doc.HeartbeatAt)
	}

	h.retire()
	if n, _ := brokers.CountDocuments(context.Background(), bson.M{"_id": h.brokerID}); n != 0 {
		t.Error("heartbeat left behind by a retired broker")
	}
}
