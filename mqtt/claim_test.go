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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	testSecret = "s3cret-of-the-device"
	testAlice  = "prn:pantahub.com:auth:/alice"
	testBob    = "prn:pantahub.com:auth:/bob"
)

func claimWaitClient(deviceID string) *mochi.Client {
	cl := &mochi.Client{}
	setIdentity(cl, kindClaimWait, deviceID, "")
	return cl
}

// Rule 2 and 3: a claim-wait session subscribes to its own claimed topic and
// to nothing else, and publishes nothing, its own status included. The ACL
// never reaches the database for it.
func TestClaimWaitACL(t *testing.T) {
	h := &authHook{} // no mongo client
	own := "5f0000000000000000000001"
	other := "5f0000000000000000000002"
	cl := claimWaitClient(own)

	if !h.OnACLCheck(cl, Topic(own, SuffixClaimed), false) {
		t.Fatal("a claim-wait session was denied its own claimed topic")
	}

	for _, filter := range []string{
		Topic(other, SuffixClaimed),
		Prefix + own + "/+",
		Prefix + own + "/#",
		Prefix + "+/" + SuffixClaimed,
		Prefix + "#",
		"ph/#",
		"#",
		"+/+/+/" + own + "/" + SuffixClaimed,
		"$share/g/" + Topic(own, SuffixClaimed),
		"$share/" + own + "/" + Topic(own, SuffixClaimed),
		"$SYS/#",
		"$SYS/broker/clients/connected",
		Topic(own, SuffixClaimed) + "/",
		Topic(own, SuffixClaimed) + "/x",
		Topic(own, SuffixStepsNew),
		Topic(own, SuffixCommands),
		Topic(own, SuffixUserMeta),
		Topic(own, SuffixStatus),
		Topic(own, SuffixDeviceMeta),
		Topic(own, SuffixCommandsResult),
		ProgressTopic(own, 1),
		"",
	} {
		if h.OnACLCheck(cl, filter, false) {
			t.Errorf("claim-wait session may subscribe to %q", filter)
		}
	}

	for _, topic := range []string{
		Topic(own, SuffixClaimed),
		Topic(own, SuffixStatus),
		Topic(own, SuffixDeviceMeta),
		Topic(own, SuffixLogs),
		Topic(own, SuffixUserMetaGet),
		Topic(own, SuffixStepsGet),
		Topic(own, SuffixCommandsResult),
		ProgressTopic(own, 3),
		Topic(other, SuffixClaimed),
		"$SYS/x",
	} {
		if h.OnACLCheck(cl, topic, true) {
			t.Errorf("claim-wait session may publish on %q", topic)
		}
	}
}

// Rule 6: nobody but the Hub publishes on a claimed topic, claimed devices do
// not use it at all, and no user subscribes to one, owned device or not. The
// user refusals come before any ownership lookup.
func TestClaimedTopicIsTheHubsAlone(t *testing.T) {
	h := &authHook{} // no mongo client: userOwns would deny, not allow
	deviceID := "5f0000000000000000000001"
	topic := Topic(deviceID, SuffixClaimed)

	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")
	if h.OnACLCheck(device, topic, true) {
		t.Error("a claimed device may publish on its claimed topic")
	}
	if h.OnACLCheck(device, topic, false) {
		t.Error("a claimed device may subscribe to its claimed topic")
	}
	if h.OnACLCheck(device, Topic("5f0000000000000000000002", SuffixClaimed), true) {
		t.Error("a device may publish on another device's claimed topic")
	}

	for _, scope := range []string{scopeAll, scopeReadOnly} {
		user := &mochi.Client{}
		setIdentity(user, kindUser, testAlice, scope)
		// Cached as owned, to prove the refusal does not rest on ownership.
		cacheOwnership(user, deviceID, true)
		if h.OnACLCheck(user, topic, false) {
			t.Errorf("a user (%s) may subscribe to a claimed topic", scope)
		}
		if h.OnACLCheck(user, topic, true) {
			t.Errorf("a user (%s) may publish on a claimed topic", scope)
		}
		if !h.OnACLCheck(user, Topic(deviceID, SuffixUserMeta), false) {
			t.Errorf("a user (%s) lost its owned device's user-meta", scope)
		}
	}
}

// Rule 6, defence in depth: were the ACL to let a publish through, the bridge
// still refuses it on a claimed topic unless the Hub itself is publishing.
func TestBridgeRefusesForgedClaims(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	topic := Topic(deviceID, SuffixClaimed)
	h := newTestBridge()

	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")
	user := &mochi.Client{}
	setIdentity(user, kindUser, testAlice, scopeAll)

	for name, cl := range map[string]*mochi.Client{
		"device": device, "claim-wait": claimWaitClient(deviceID), "user": user, "anonymous": {},
	} {
		pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: 1}, TopicName: topic, Payload: []byte(`{"owner":"x"}`)}
		if _, err := h.OnPublish(cl, pk); !errors.Is(err, packets.ErrRejectPacket) {
			t.Errorf("%s forged a claim", name)
		}
	}

	inline := &mochi.Client{}
	inline.Net.Inline = true
	pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: 1}, TopicName: topic, Payload: []byte(`{"owner":"x"}`)}
	if _, err := h.OnPublish(inline, pk); err != nil {
		t.Errorf("the Hub's claim refused: %v", err)
	}
	pk.FixedHeader.Retain = true
	if _, err := h.OnPublish(inline, pk); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("the Hub's claim was retained")
	}
}

// Rules 4 and 5: the claimed topic is never retained, and a claim-wait
// session retains nothing, its status included.
func TestClaimRetention(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	inline := &mochi.Client{}
	inline.Net.Inline = true
	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")

	if mayRetain(inline, Topic(deviceID, SuffixClaimed)) {
		t.Error("the claimed topic may be retained by the Hub")
	}
	if mayRetain(claimWaitClient(deviceID), Topic(deviceID, SuffixStatus)) {
		t.Error("a claim-wait session may retain its status")
	}
	if mayRetain(nil, Topic(deviceID, SuffixStatus)) {
		t.Error("an unknown publisher may retain")
	}
	if !mayRetain(device, Topic(deviceID, SuffixStatus)) {
		t.Error("a claimed device may no longer retain its status")
	}
	if !mayRetain(inline, Topic(deviceID, SuffixStepsNew)) {
		t.Error("the Hub may no longer retain steps/new")
	}
}

// Rule 4 at the unit level: no will, clean sessions only, within the cap;
// the admitted session ends with its connection.
func TestAdmitClaimWait(t *testing.T) {
	deviceID := "5f0000000000000000000001"
	clean := packets.Packet{Connect: packets.ConnectParams{Clean: true, Keepalive: 60, ClientIdentifier: deviceID}}

	if (&authHook{}).admitClaimWait(claimWaitClient(deviceID), clean) {
		t.Fatal("admitted without a claim hook")
	}

	h := &authHook{claims: newClaimHook(nil, nil, 1)}
	for name, pk := range map[string]packets.Packet{
		"will":           {Connect: packets.ConnectParams{Clean: true, Keepalive: 60, WillFlag: true, WillTopic: Topic(deviceID, SuffixStatus), WillPayload: []byte("offline")}},
		"retained will":  {Connect: packets.ConnectParams{Clean: true, Keepalive: 60, WillFlag: true, WillRetain: true, WillTopic: Topic(deviceID, SuffixStatus)}},
		"will topic":     {Connect: packets.ConnectParams{Clean: true, Keepalive: 60, WillTopic: Topic(deviceID, SuffixStatus)}},
		"persistent":     {Connect: packets.ConnectParams{Clean: false, Keepalive: 60}},
		"no keepalive":   {Connect: packets.ConnectParams{Clean: true, Keepalive: 0}},
		"long keepalive": {Connect: packets.ConnectParams{Clean: true, Keepalive: maxClaimWaitKeepalive + 1}},
	} {
		if h.admitClaimWait(claimWaitClient(deviceID), pk) {
			t.Errorf("claim-wait session with a %s admitted", name)
		}
	}

	cl := claimWaitClient(deviceID)
	cl.Properties.Props.SessionExpiryInterval = 3600
	cl.Properties.Props.SessionExpiryIntervalFlag = true
	if !h.admitClaimWait(cl, clean) {
		t.Fatal("a clean claim-wait session without a will was refused")
	}
	if !cl.Properties.Clean || cl.Properties.Props.SessionExpiryInterval != 0 || cl.Properties.Props.SessionExpiryIntervalFlag {
		t.Error("the claim-wait session may outlive its connection")
	}

	h.claims.OnSessionEstablished(cl, packets.Packet{})
	if h.admitClaimWait(claimWaitClient("5f0000000000000000000002"), clean) {
		t.Error("a claim-wait session admitted over the cap")
	}
	h.claims.OnDisconnect(cl, nil, true)
	if !h.admitClaimWait(claimWaitClient("5f0000000000000000000002"), clean) {
		t.Error("a claim-wait session refused under the cap")
	}
}

// Rule 9: one session per device across its claim-wait and claimed sessions,
// but never across devices or towards users.
func TestClaimWaitSessionTakeover(t *testing.T) {
	server := mochi.New(&mochi.Options{InlineClient: true, Logger: discardLogger})
	h := &authHook{server: server}
	deviceID := "5f0000000000000000000001"

	waiting := server.NewClient(nil, "test", deviceID, false)
	setIdentity(waiting, kindClaimWait, deviceID, "")
	server.Clients.Add(waiting)

	claimed := server.NewClient(nil, "test", deviceID, false)
	setIdentity(claimed, kindDevice, deviceID, "")
	if !h.mayTakeOverSession(claimed) {
		t.Fatal("a device just claimed may not replace its claim-wait session")
	}

	again := server.NewClient(nil, "test", deviceID, false)
	setIdentity(again, kindClaimWait, deviceID, "")
	if !h.mayTakeOverSession(again) {
		t.Fatal("a device may not reconnect its claim-wait session")
	}

	other := server.NewClient(nil, "test", deviceID, false)
	setIdentity(other, kindClaimWait, "5f0000000000000000000002", "")
	if h.mayTakeOverSession(other) {
		t.Fatal("another device took over a claim-wait session")
	}
	if mayClaimSession(other, deviceID) {
		t.Fatal("a claim-wait session opened under another device's id")
	}

	user := server.NewClient(nil, "test", deviceID, false)
	setIdentity(user, kindUser, testAlice, scopeAll)
	if h.mayTakeOverSession(user) {
		t.Fatal("a user took over a claim-wait session")
	}
}

func TestClaimedOwner(t *testing.T) {
	event := func(fields bson.M) *changeEvent {
		raw, err := bson.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		e := &changeEvent{OperationType: "update"}
		e.UpdateDescription.UpdatedFields = raw
		return e
	}
	if owner, ok := claimedOwner(event(bson.M{"owner": testAlice, "challenge": ""})); !ok || owner != testAlice {
		t.Errorf("claim not seen: %q %v", owner, ok)
	}
	for name, fields := range map[string]bson.M{
		"no owner":    {"nick": "x"},
		"owner unset": {"owner": ""},
		"not string":  {"owner": 1},
	} {
		if _, ok := claimedOwner(event(fields)); ok {
			t.Errorf("%s taken for a claim", name)
		}
	}
}

func TestMaxClaimWaitSessions(t *testing.T) {
	for raw, want := range map[string]int{"": defaultMaxClaimWaitSessions, "0": 0, "12": 12, "-1": defaultMaxClaimWaitSessions, "x": defaultMaxClaimWaitSessions} {
		t.Setenv(EnvMqttMaxClaimWaitSessions, raw)
		if got := maxClaimWaitSessions(); got != want {
			t.Errorf("%q: %d, want %d", raw, got, want)
		}
	}
}

// --- Over the wire, against MongoDB -----------------------------------------

// claimBroker is one broker replica with the production auth, claim,
// presence and bridge hooks.
type claimBroker struct {
	server   *mochi.Server
	claims   *claimHook
	notifier *Notifier
	events   *eventRecorder
}

func newClaimBroker(t *testing.T, client *mongo.Client, brokerID string, maxSessions int) *claimBroker {
	t.Helper()
	// Every CONNECT tries its password as a token first, and the API's key
	// is loaded once per process: it must be the tests' key from the start.
	testJWTKey(t)
	server := mochi.New(&mochi.Options{InlineClient: true, Logger: discardLogger})
	b := &claimBroker{server: server, events: newEventRecorder()}
	if maxSessions > 0 {
		b.claims = newClaimHook(client, server, maxSessions)
	}
	bridge := newTestBridge()
	bridge.jobs = make(chan bridgeJob, bridgeQueueDepth)
	bridge.mongoClient = client

	hooks := []mochi.Hook{&authHook{mongoClient: client, server: server, claims: b.claims}}
	if b.claims != nil {
		hooks = append(hooks, b.claims)
	}
	hooks = append(hooks,
		&presenceHook{mongoClient: client, server: server, brokerID: brokerID},
		uninitialisedBridge{bridge},
		b.events,
	)
	for _, hook := range hooks {
		if err := server.AddHook(hook, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.Serve(); err != nil {
		t.Fatal(err)
	}
	b.notifier = NewNotifier(client, server)
	b.notifier.claims = b.claims
	t.Cleanup(func() { server.Close() })
	return b
}

// runNotifier starts the change streams and waits until the devices stream
// is delivering: an update of a scratch device's user-meta shows up as that
// device's retained user-meta.
func (b *claimBroker) runNotifier(t *testing.T, client *mongo.Client) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go b.notifier.Run(ctx)

	probe := primitive.NewObjectID()
	devicesColl := client.Database(utils.MongoDb).Collection(devicesCollection)
	if _, err := devicesColl.InsertOne(context.Background(), bson.M{"_id": probe, "owner": testAlice}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for i := 0; ; i++ {
		_, err := devicesColl.UpdateOne(context.Background(), bson.M{"_id": probe}, bson.M{"$set": bson.M{"user-meta.probe": strconv.Itoa(i)}})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if _, ok := b.server.Topics.Retained.Get(Topic(probe.Hex(), SuffixUserMeta)); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the devices change stream never opened")
		}
	}
}

func (b *claimBroker) waitDisconnect(t *testing.T) *mochi.Client {
	t.Helper()
	select {
	case cl := <-b.events.disconnects:
		return cl
	case <-time.After(10 * time.Second):
		t.Fatal("no disconnect")
		return nil
	}
}

// insertDevice stores a device with a known secret. An empty owner makes it
// unclaimed, as registration leaves it.
func insertDevice(t *testing.T, client *mongo.Client, owner string, extra bson.M) primitive.ObjectID {
	t.Helper()
	id := primitive.NewObjectID()
	doc := bson.M{
		"_id": id, "prn": devicePrnPrefix + id.Hex(), "nick": "__unregistered__" + id.Hex(),
		"owner": owner, "secret": testSecret, "challenge": "fancy-badger", "device-meta": bson.M{},
	}
	for k, v := range extra {
		doc[k] = v
	}
	if _, err := client.Database(utils.MongoDb).Collection(devicesCollection).InsertOne(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	return id
}

// claim does what PUT /devices/{id} does once the challenge matches.
func claim(t *testing.T, client *mongo.Client, id primitive.ObjectID, owner string) {
	t.Helper()
	_, err := client.Database(utils.MongoDb).Collection(devicesCollection).UpdateOne(context.Background(),
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"owner": owner, "challenge": "", "nick": "claimed-" + id.Hex(), "timemodified": time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
}

func signToken(t *testing.T, claims jwtgo.MapClaims) string {
	t.Helper()
	key := testJWTKey(t)
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func deviceToken(t *testing.T, id primitive.ObjectID) string {
	return signToken(t, jwtgo.MapClaims{"type": "DEVICE", "prn": devicePrnPrefix + id.Hex(), "owner": "", "scopes": scopeAll})
}

// wireClient is a bare MQTT client on an in-memory connection to a broker.
type wireClient struct {
	conn    net.Conn
	version byte
	in      chan packets.Packet
	// pending holds packets that arrived while waiting for a SUBACK: the
	// broker may send a matching PUBLISH before it.
	pending []packets.Packet
}

// connectParams are a device's CONNECT as the pv-mqtt-sdk agent sends it in
// claim-wait: its PRN and secret, its id as client id, a clean session.
func connectParams(id primitive.ObjectID) packets.ConnectParams {
	return packets.ConnectParams{
		Clean:            true,
		Keepalive:        60,
		ClientIdentifier: id.Hex(),
		Username:         []byte(devicePrnPrefix + id.Hex()),
		Password:         []byte(testSecret),
		UsernameFlag:     true,
		PasswordFlag:     true,
	}
}

// dialWire connects and returns the client and its CONNACK reason code.
func dialWire(t *testing.T, server *mochi.Server, version byte, params packets.ConnectParams) (*wireClient, byte) {
	t.Helper()
	srv, cli := net.Pipe()
	go server.EstablishConnection("test", srv)
	c := &wireClient{conn: cli, version: version, in: make(chan packets.Packet, 64)}
	go c.readLoop()
	t.Cleanup(func() { cli.Close() })

	params.ProtocolName = []byte("MQTT")
	params.UsernameFlag = len(params.Username) > 0
	params.PasswordFlag = len(params.Password) > 0
	c.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Connect}, ProtocolVersion: version, Connect: params})
	connack, ok := c.next(5 * time.Second)
	if !ok {
		// A refused CONNECT may close before the CONNACK is read.
		return c, packets.ErrNotAuthorized.Code
	}
	if connack.FixedHeader.Type != packets.Connack {
		t.Fatalf("expected CONNACK, got packet type %d", connack.FixedHeader.Type)
	}
	return c, connack.ReasonCode
}

func (c *wireClient) readLoop() {
	defer close(c.in)
	r := bufio.NewReader(c.conn)
	for {
		first, err := r.ReadByte()
		if err != nil {
			return
		}
		fh := packets.FixedHeader{}
		if err := fh.Decode(first); err != nil {
			return
		}
		remaining, multiplier := 0, 1
		for {
			b, err := r.ReadByte()
			if err != nil {
				return
			}
			remaining += int(b&127) * multiplier
			if b&128 == 0 {
				break
			}
			multiplier *= 128
		}
		fh.Remaining = remaining
		body := make([]byte, remaining)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}

		pk := packets.Packet{FixedHeader: fh, ProtocolVersion: c.version}
		switch fh.Type {
		case packets.Connack:
			err = pk.ConnackDecode(body)
		case packets.Suback:
			err = pk.SubackDecode(body)
		case packets.Publish:
			err = pk.PublishDecode(body)
		case packets.Disconnect:
			if remaining > 0 {
				err = pk.DisconnectDecode(body)
			}
		}
		if err != nil {
			return
		}
		c.in <- pk
	}
}

func (c *wireClient) send(t *testing.T, pk packets.Packet) error {
	t.Helper()
	pk.ProtocolVersion = c.version
	var buf bytes.Buffer
	var err error
	switch pk.FixedHeader.Type {
	case packets.Connect:
		err = pk.ConnectEncode(&buf)
	case packets.Subscribe:
		err = pk.SubscribeEncode(&buf)
	case packets.Publish:
		err = pk.PublishEncode(&buf)
	case packets.Puback:
		err = pk.PubackEncode(&buf)
	}
	if err != nil {
		t.Fatal(err)
	}
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = c.conn.Write(buf.Bytes())
	return err
}

// next returns the next packet from the broker; ok is false once the broker
// closed the connection or nothing came within d.
func (c *wireClient) next(d time.Duration) (packets.Packet, bool) {
	if len(c.pending) > 0 {
		pk := c.pending[0]
		c.pending = c.pending[1:]
		return pk, true
	}
	select {
	case pk, ok := <-c.in:
		return pk, ok
	case <-time.After(d):
		return packets.Packet{}, false
	}
}

// subscribe sends one SUBSCRIBE and returns the SUBACK reason codes.
func (c *wireClient) subscribe(t *testing.T, filters ...string) []byte {
	t.Helper()
	subs := packets.Subscriptions{}
	for _, f := range filters {
		subs = append(subs, packets.Subscription{Filter: f, Qos: 1})
	}
	if err := c.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Subscribe, Qos: 1}, PacketID: 7, Filters: subs}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	for {
		select {
		case pk, ok := <-c.in:
			if !ok {
				t.Fatal("closed before the SUBACK")
			}
			if pk.FixedHeader.Type == packets.Suback {
				return pk.ReasonCodes
			}
			c.pending = append(c.pending, pk)
		case <-time.After(5 * time.Second):
			t.Fatal("no SUBACK")
		}
	}
}

func (c *wireClient) publish(t *testing.T, topic string, payload string, qos byte, retain bool) {
	t.Helper()
	pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: qos, Retain: retain}, TopicName: topic, Payload: []byte(payload)}
	if qos > 0 {
		pk.PacketID = 9
	}
	c.send(t, pk)
}

// expectClosed waits for the broker to close the connection, allowing only a
// DISCONNECT before it.
func (c *wireClient) expectClosed(t *testing.T) {
	t.Helper()
	for _, pk := range c.pending {
		if pk.FixedHeader.Type != packets.Disconnect {
			t.Fatalf("packet type %d (topic %q) while waiting for the close", pk.FixedHeader.Type, pk.TopicName)
		}
	}
	c.pending = nil
	deadline := time.After(10 * time.Second)
	for {
		select {
		case pk, ok := <-c.in:
			if !ok {
				return
			}
			if pk.FixedHeader.Type != packets.Disconnect {
				t.Fatalf("packet type %d (topic %q) while waiting for the close", pk.FixedHeader.Type, pk.TopicName)
			}
		case <-deadline:
			t.Fatal("the broker kept the connection open")
		}
	}
}

// expectSilence fails on any packet within d.
func (c *wireClient) expectSilence(t *testing.T, d time.Duration) {
	t.Helper()
	if pk, ok := c.next(d); ok {
		t.Fatalf("unexpected packet type %d on %q: %s", pk.FixedHeader.Type, pk.TopicName, pk.Payload)
	}
}

func granted(code byte) bool { return code < packets.ErrUnspecifiedError.Code }

// Rule 1 and 4 over the wire: who may open a claim-wait session.
func TestClaimWaitConnect(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	accepted := packets.CodeSuccess.Code

	device := insertDevice(t, client, "", nil)
	other := insertDevice(t, client, "", nil)

	for name, tc := range map[string]struct {
		mutate func(*packets.ConnectParams)
		want   bool
	}{
		"prn and secret":      {func(p *packets.ConnectParams) {}, true},
		"device token":        {func(p *packets.ConnectParams) { p.Password = []byte(deviceToken(t, device)) }, true},
		"wrong secret":        {func(p *packets.ConnectParams) { p.Password = []byte("nope") }, false},
		"another's username":  {func(p *packets.ConnectParams) { p.Username = []byte(devicePrnPrefix + other.Hex()) }, false},
		"another's token":     {func(p *packets.ConnectParams) { p.Password = []byte(deviceToken(t, other)) }, false},
		"another's client id": {func(p *packets.ConnectParams) { p.ClientIdentifier = other.Hex() }, false},
		"free client id":      {func(p *packets.ConnectParams) { p.ClientIdentifier = "agent" }, false},
		"user token": {func(p *packets.ConnectParams) {
			p.Password = []byte(signToken(t, jwtgo.MapClaims{"type": "USER", "prn": testAlice, "scopes": scopeAll}))
		}, false},
		"will": {func(p *packets.ConnectParams) {
			p.WillFlag, p.WillTopic, p.WillPayload = true, Topic(device.Hex(), SuffixStatus), []byte("offline")
		}, false},
		"retained will": {func(p *packets.ConnectParams) {
			p.WillFlag, p.WillRetain, p.WillTopic, p.WillPayload = true, true, Topic(device.Hex(), SuffixStatus), []byte("offline")
		}, false},
		"will on claimed": {func(p *packets.ConnectParams) {
			p.WillFlag, p.WillTopic, p.WillPayload = true, Topic(device.Hex(), SuffixClaimed), []byte(`{"owner":"x"}`)
		}, false},
		"persistent session": {func(p *packets.ConnectParams) { p.Clean = false }, false},
		"no keepalive":       {func(p *packets.ConnectParams) { p.Keepalive = 0 }, false},
		"long keepalive":     {func(p *packets.ConnectParams) { p.Keepalive = maxClaimWaitKeepalive + 1 }, false},
		"longest keepalive":  {func(p *packets.ConnectParams) { p.Keepalive = maxClaimWaitKeepalive }, true},
		"no password":        {func(p *packets.ConnectParams) { p.Password = nil }, false},
		"not a device prn":   {func(p *packets.ConnectParams) { p.Username = []byte(testAlice) }, false},
	} {
		params := connectParams(device)
		tc.mutate(&params)
		c, code := dialWire(t, b.server, 4, params)
		if got := code == accepted; got != tc.want {
			t.Errorf("%s: accepted %v, want %v (code %#x)", name, got, tc.want, code)
		}
		c.conn.Close()
		if code == accepted {
			b.waitDisconnect(t)
		}
	}

	// Ownership verification pending keeps refusing the device, claimed or not.
	for _, owner := range []string{"", testAlice} {
		pending := insertDevice(t, client, owner, bson.M{"ovmode": bson.M{"mode": "tls", "status": "pending"}})
		c, code := dialWire(t, b.server, 4, connectParams(pending))
		if code == accepted {
			t.Errorf("device (owner %q) pending ownership verification accepted", owner)
		}
		c.conn.Close()
	}

	// A deleted device is gone for good.
	gone := insertDevice(t, client, "", bson.M{"garbage": true})
	if c, code := dialWire(t, b.server, 4, connectParams(gone)); code == accepted {
		t.Error("a garbage device accepted")
		c.conn.Close()
	}

	// A replica that serves no claim-wait sessions refuses unclaimed devices.
	off := newClaimBroker(t, client, "off", 0)
	if c, code := dialWire(t, off.server, 4, connectParams(device)); code == accepted {
		t.Error("an unclaimed device accepted with claim-wait sessions off")
		c.conn.Close()
	}
}

// Rules 2, 3 and 5 over the wire: what an open claim-wait session can do.
func TestClaimWaitSession(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	device := insertDevice(t, client, "", nil)
	other := insertDevice(t, client, "", nil)
	own := device.Hex()

	// Retained state on every topic the device will be refused, including its
	// own, waiting to leak.
	for _, topic := range []string{
		Topic(own, SuffixStepsNew), Topic(own, SuffixUserMeta), Topic(own, SuffixStatus),
		Topic(other.Hex(), SuffixStepsNew), "$SYS/secret", "ph/v1/other",
	} {
		if err := b.server.Publish(topic, []byte(`{"leak":true}`), true, 1); err != nil {
			t.Fatal(err)
		}
	}

	c, code := dialWire(t, b.server, 4, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("claim-wait CONNECT refused: %#x", code)
	}
	session, ok := b.server.Clients.Get(own)
	if !ok {
		t.Fatal("no session")
	}
	if kind, _ := identity(session); kind != kindClaimWait {
		t.Fatalf("session kind %q", kind)
	}
	if !session.Properties.Clean || session.Properties.Props.SessionExpiryInterval != 0 {
		t.Fatal("the claim-wait session outlives its connection")
	}

	refused := []string{
		Topic(other.Hex(), SuffixClaimed),
		Prefix + own + "/+",
		Prefix + own + "/#",
		Prefix + "+/" + SuffixClaimed,
		"#",
		"$share/g/" + Topic(own, SuffixClaimed),
		"$SYS/#",
		Topic(own, SuffixStepsNew),
		Topic(own, SuffixCommands),
		Topic(own, SuffixUserMeta),
		Topic(own, SuffixStatus),
	}
	codes := c.subscribe(t, append([]string{Topic(own, SuffixClaimed)}, refused...)...)
	if len(codes) != len(refused)+1 || !granted(codes[0]) {
		t.Fatalf("own claimed topic not granted: %v", codes)
	}
	for i, filter := range refused {
		if granted(codes[i+1]) {
			t.Errorf("subscription to %q granted", filter)
		}
	}
	// Nothing retained anywhere reaches the session.
	c.expectSilence(t, 300*time.Millisecond)
	if subs := session.State.Subscriptions.GetAll(); len(subs) != 1 {
		t.Errorf("%d subscriptions held, want only the claimed topic", len(subs))
	}

	// Publishing at QoS 0 is dropped: no retained copy, no delivery.
	c.publish(t, Topic(own, SuffixClaimed), `{"owner":"`+testBob+`"}`, 0, true)
	c.publish(t, Topic(own, SuffixStatus), `{"online":true}`, 0, true)
	c.expectSilence(t, 300*time.Millisecond)
	if _, retained := b.server.Topics.Retained.Get(Topic(own, SuffixClaimed)); retained {
		t.Error("the device retained a claim")
	}
	if pk, _ := b.server.Topics.Retained.Get(Topic(own, SuffixStatus)); string(pk.Payload) != `{"leak":true}` {
		t.Errorf("the claim-wait session wrote its status: %s", pk.Payload)
	}

	// Publishing at QoS 1, its own status included, ends the session (MQTT
	// 3.1.1 has no refusal code for a PUBLISH).
	c.publish(t, Topic(own, SuffixStatus), `{"online":true}`, 1, false)
	c.expectClosed(t)
	b.waitDisconnect(t)
	// The broker drops the session right after OnDisconnect.
	deadline := time.Now().Add(5 * time.Second)
	for _, ok := b.server.Clients.Get(own); ok; _, ok = b.server.Clients.Get(own) {
		if time.Now().After(deadline) {
			t.Fatal("the claim-wait session survived its connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if b.claims.count() != 0 {
		t.Errorf("%d claim-wait sessions counted after the disconnect", b.claims.count())
	}
}

// Claimed devices keep their persistent sessions, wills and full ACL, and
// neither they nor users can use a claimed topic.
func TestClaimedDevicesAndUsersUnaffected(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	device := insertDevice(t, client, testAlice, nil)
	own := device.Hex()

	params := connectParams(device)
	params.Clean = false
	params.WillFlag, params.WillRetain = true, true
	params.WillTopic, params.WillPayload = Topic(own, SuffixStatus), []byte(`{"online":false,"status":"offline"}`)
	c, code := dialWire(t, b.server, 4, params)
	if code != packets.CodeSuccess.Code {
		t.Fatalf("claimed device refused: %#x", code)
	}
	session, _ := b.server.Clients.Get(own)
	if kind, _ := identity(session); kind != kindDevice {
		t.Fatalf("claimed device got kind %q", kind)
	}
	if session.Properties.Clean {
		t.Error("the claimed device's persistent session was made clean")
	}
	codes := c.subscribe(t, Topic(own, SuffixStepsNew), Topic(own, SuffixUserMeta), Topic(own, SuffixCommands), Topic(own, SuffixClaimed))
	if !granted(codes[0]) || !granted(codes[1]) || !granted(codes[2]) {
		t.Errorf("claimed device lost a subscription: %v", codes)
	}
	if granted(codes[3]) {
		t.Error("claimed device subscribed to its claimed topic")
	}
	c.publish(t, Topic(own, SuffixClaimed), `{"owner":"`+testBob+`"}`, 1, false)
	c.expectClosed(t)
	b.waitDisconnect(t)

	// A user, owner or not, cannot subscribe to a claimed topic; the owner
	// still reads its device.
	stranger := insertDevice(t, client, testBob, nil)
	unclaimed := insertDevice(t, client, "", nil)
	userParams := packets.ConnectParams{
		Clean: true, Keepalive: 60, ClientIdentifier: "dashboard-alice",
		Username: []byte(devicePrnPrefix + own),
		Password: []byte(signToken(t, jwtgo.MapClaims{"type": "USER", "prn": testAlice, "scopes": scopeAll})),
	}
	u, code := dialWire(t, b.server, 4, userParams)
	if code != packets.CodeSuccess.Code {
		t.Fatalf("user refused: %#x", code)
	}
	codes = u.subscribe(t, Topic(own, SuffixUserMeta), Topic(own, SuffixClaimed), Topic(stranger.Hex(), SuffixClaimed), Topic(unclaimed.Hex(), SuffixClaimed))
	if !granted(codes[0]) {
		t.Error("owner lost its device's user-meta")
	}
	for i, what := range []string{"own device's", "another user's device's", "unclaimed device's"} {
		if granted(codes[i+1]) {
			t.Errorf("user subscribed to its %s claimed topic", what)
		}
	}
	u.publish(t, Topic(unclaimed.Hex(), SuffixClaimed), `{"owner":"`+testAlice+`"}`, 1, false)
	u.expectClosed(t)
}

// The positive flow, rule 7: claim, the claimed message, the close, and a
// reconnect that gets the claimed-device ACL.
func TestClaimFlow(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	b.runNotifier(t, client)

	device := insertDevice(t, client, "", nil)
	own := device.Hex()
	c, code := dialWire(t, b.server, 4, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("claim-wait CONNECT refused: %#x", code)
	}
	if codes := c.subscribe(t, Topic(own, SuffixClaimed)); !granted(codes[0]) {
		t.Fatalf("claimed topic refused: %v", codes)
	}

	claim(t, client, device, testAlice)

	pk, ok := c.next(15 * time.Second)
	if !ok || pk.FixedHeader.Type != packets.Publish {
		t.Fatalf("no claimed message (got %v, type %d)", ok, pk.FixedHeader.Type)
	}
	if pk.TopicName != Topic(own, SuffixClaimed) || pk.FixedHeader.Qos != 1 || pk.FixedHeader.Retain {
		t.Errorf("claimed message on %q, QoS %d, retain %v", pk.TopicName, pk.FixedHeader.Qos, pk.FixedHeader.Retain)
	}
	notice := map[string]interface{}{}
	if err := json.Unmarshal(pk.Payload, &notice); err != nil || len(notice) != 1 || notice["owner"] != testAlice {
		t.Errorf("payload %s", pk.Payload)
	}
	if _, retained := b.server.Topics.Retained.Get(Topic(own, SuffixClaimed)); retained {
		t.Error("the claim was retained")
	}

	// Until the PUBACK the session stays; right after, the broker closes it.
	c.expectSilence(t, 200*time.Millisecond)
	c.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Puback}, PacketID: pk.PacketID})
	c.expectClosed(t)
	if cl := b.waitDisconnect(t); cl.ID != own {
		t.Fatalf("disconnect of %q", cl.ID)
	}
	if b.claims.count() != 0 {
		t.Error("the claim-wait session is still counted")
	}

	// The next connection is a claimed device's, with a persistent session
	// and a will, and the full device ACL.
	params := connectParams(device)
	params.Clean = false
	params.WillFlag, params.WillRetain = true, true
	params.WillTopic, params.WillPayload = Topic(own, SuffixStatus), []byte(`{"online":false,"status":"offline"}`)
	d, code := dialWire(t, b.server, 4, params)
	if code != packets.CodeSuccess.Code {
		t.Fatalf("the claimed device was refused: %#x", code)
	}
	session, _ := b.server.Clients.Get(own)
	if kind, _ := identity(session); kind != kindDevice {
		t.Fatalf("reconnect kind %q", kind)
	}
	codes := d.subscribe(t, Topic(own, SuffixStepsNew), Topic(own, SuffixCommands), Topic(own, SuffixUserMeta), Topic(own, SuffixClaimed))
	if !granted(codes[0]) || !granted(codes[1]) || !granted(codes[2]) || granted(codes[3]) {
		t.Errorf("claimed device ACL: %v", codes)
	}
	d.publish(t, Topic(own, SuffixStatus), `{"online":true,"status":"online"}`, 1, true)
	if ack, ok := d.next(5 * time.Second); !ok || ack.FixedHeader.Type != packets.Puback {
		t.Fatalf("the claimed device's status was not acknowledged: %v %d", ok, ack.FixedHeader.Type)
	}
	d.expectSilence(t, 300*time.Millisecond)
	if _, ok := b.server.Clients.Get(own); !ok {
		t.Error("the claimed device lost its session publishing its status")
	}
}

// Rule 7: a claim-wait session never widens in place. A device claimed while
// no one told its session (no change stream, no sweep yet) keeps the
// claim-wait ACL until the broker closes it.
func TestClaimWaitNeverWidens(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	device := insertDevice(t, client, "", nil)
	own := device.Hex()

	c, _ := dialWire(t, b.server, 4, connectParams(device))
	claim(t, client, device, testAlice)

	codes := c.subscribe(t, Topic(own, SuffixCommands), Topic(own, SuffixStepsNew), Topic(own, SuffixUserMeta))
	for i, code := range codes {
		if granted(code) {
			t.Errorf("claimed device's filter %d granted to its claim-wait session", i)
		}
	}
	c.publish(t, Topic(own, SuffixStatus), `{"online":true}`, 1, false)
	c.expectClosed(t)
}

// A claim that lands between CONNECT and SUBSCRIBE is caught by the
// subscribe check.
func TestClaimBeforeSubscribe(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	device := insertDevice(t, client, "", nil)
	own := device.Hex()

	c, _ := dialWire(t, b.server, 5, connectParams(device))
	claim(t, client, device, testBob)
	if codes := c.subscribe(t, Topic(own, SuffixClaimed)); !granted(codes[0]) {
		t.Fatalf("claimed topic refused: %v", codes)
	}
	pk, ok := c.next(10 * time.Second)
	if !ok || pk.FixedHeader.Type != packets.Publish || !bytes.Contains(pk.Payload, []byte(testBob)) {
		t.Fatalf("no claimed message: %v %d %s", ok, pk.FixedHeader.Type, pk.Payload)
	}
	c.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Puback}, PacketID: pk.PacketID})
	// MQTT 5 is told why.
	disconnect, ok := c.next(10 * time.Second)
	if !ok || disconnect.FixedHeader.Type != packets.Disconnect || disconnect.ReasonCode != packets.ErrAdministrativeAction.Code {
		t.Errorf("no administrative DISCONNECT: %v %d %#x", ok, disconnect.FixedHeader.Type, disconnect.ReasonCode)
	}
	c.expectClosed(t)
}

// The sweep ends the sessions of devices claimed or deleted without an event
// reaching this replica, and leaves the others be. A device that does not
// acknowledge its claim is closed anyway once the wait is over.
func TestClaimSweep(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 100)
	b.claims.ackTimeout = 300 * time.Millisecond

	claimedLater := insertDevice(t, client, "", nil)
	deleted := insertDevice(t, client, "", nil)
	waiting := insertDevice(t, client, "", nil)

	conns := map[primitive.ObjectID]*wireClient{}
	for _, id := range []primitive.ObjectID{claimedLater, deleted, waiting} {
		c, code := dialWire(t, b.server, 4, connectParams(id))
		if code != packets.CodeSuccess.Code {
			t.Fatalf("CONNECT refused: %#x", code)
		}
		if codes := c.subscribe(t, Topic(id.Hex(), SuffixClaimed)); !granted(codes[0]) {
			t.Fatal("claimed topic refused")
		}
		conns[id] = c
	}

	claim(t, client, claimedLater, testAlice)
	if _, err := client.Database(utils.MongoDb).Collection(devicesCollection).UpdateOne(context.Background(),
		bson.M{"_id": deleted}, bson.M{"$set": bson.M{"garbage": true}}); err != nil {
		t.Fatal(err)
	}
	b.claims.sweep(context.Background())

	pk, ok := conns[claimedLater].next(10 * time.Second)
	if !ok || pk.FixedHeader.Type != packets.Publish || pk.TopicName != Topic(claimedLater.Hex(), SuffixClaimed) {
		t.Fatalf("no claimed message from the sweep")
	}
	// No PUBACK: closed once the wait is over.
	conns[claimedLater].expectClosed(t)
	conns[deleted].expectClosed(t)
	conns[waiting].expectSilence(t, 500*time.Millisecond)
	if _, ok := b.server.Clients.Get(waiting.Hex()); !ok {
		t.Error("an unclaimed device's session was closed")
	}
	if n := b.claims.count(); n != 1 {
		t.Errorf("%d claim-wait sessions left, want 1", n)
	}
}

// Rule 9: the per-replica cap.
func TestClaimWaitCap(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 1)
	first := insertDevice(t, client, "", nil)
	second := insertDevice(t, client, "", nil)
	claimed := insertDevice(t, client, testAlice, nil)

	c, code := dialWire(t, b.server, 4, connectParams(first))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("first claim-wait session refused: %#x", code)
	}
	if _, code := dialWire(t, b.server, 4, connectParams(second)); code == packets.CodeSuccess.Code {
		t.Fatal("a claim-wait session admitted over the cap")
	}
	// The cap is on claim-wait sessions only.
	if _, code := dialWire(t, b.server, 4, connectParams(claimed)); code != packets.CodeSuccess.Code {
		t.Fatalf("a claimed device refused at the claim-wait cap: %#x", code)
	}

	c.conn.Close()
	if cl := b.waitDisconnect(t); cl.ID != first.Hex() {
		t.Fatalf("disconnect of %q", cl.ID)
	}
	if _, code := dialWire(t, b.server, 4, connectParams(second)); code != packets.CodeSuccess.Code {
		t.Fatalf("claim-wait session refused under the cap: %#x", code)
	}
}

// Two replicas, one database: the claim is made through replica B's REST
// plane (here, the same update), the device waits on replica A. A tells it;
// B, which holds another device's claim-wait session, leaves that one be.
func TestClaimAcrossReplicas(t *testing.T) {
	client := newTestMongo(t)
	a := newClaimBroker(t, client, "A", 100)
	b := newClaimBroker(t, client, "B", 100)
	a.runNotifier(t, client)
	b.runNotifier(t, client)

	device := insertDevice(t, client, "", nil)
	bystander := insertDevice(t, client, "", nil)

	onA, _ := dialWire(t, a.server, 4, connectParams(device))
	if codes := onA.subscribe(t, Topic(device.Hex(), SuffixClaimed)); !granted(codes[0]) {
		t.Fatal("claimed topic refused on A")
	}
	onB, _ := dialWire(t, b.server, 4, connectParams(bystander))
	if codes := onB.subscribe(t, Topic(bystander.Hex(), SuffixClaimed)); !granted(codes[0]) {
		t.Fatal("claimed topic refused on B")
	}

	claim(t, client, device, testAlice)

	pk, ok := onA.next(15 * time.Second)
	if !ok || pk.FixedHeader.Type != packets.Publish || !bytes.Contains(pk.Payload, []byte(testAlice)) {
		t.Fatalf("A did not deliver the claim: %v %d %s", ok, pk.FixedHeader.Type, pk.Payload)
	}
	onA.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Puback}, PacketID: pk.PacketID})
	onA.expectClosed(t)

	onB.expectSilence(t, time.Second)
	if _, ok := b.server.Clients.Get(bystander.Hex()); !ok {
		t.Error("B closed an unclaimed device's session")
	}

	// And the other way round: B's device is claimed, B tells it.
	claim(t, client, bystander, testBob)
	pk, ok = onB.next(15 * time.Second)
	if !ok || pk.FixedHeader.Type != packets.Publish || !bytes.Contains(pk.Payload, []byte(testBob)) {
		t.Fatalf("B did not deliver the claim")
	}
	onB.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Puback}, PacketID: pk.PacketID})
	onB.expectClosed(t)
}
