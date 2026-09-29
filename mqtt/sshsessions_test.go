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
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const testSSHPubkey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGb2HbVwUjXw2Hk0mUq1S6v9m5l8bJgkq1Vv0b0L1l7a"

func downTopic(deviceID string, session primitive.ObjectID) string {
	return SSHDataTopic(deviceID, session.Hex(), sshDown)
}

func sshUpTopic(deviceID string, session primitive.ObjectID) string {
	return SSHDataTopic(deviceID, session.Hex(), sshUp)
}

func TestSSHTopics(t *testing.T) {
	sid := primitive.NewObjectID()
	for suffix, want := range map[string]string{
		"ssh/" + sid.Hex() + "/up":   sshUp,
		"ssh/" + sid.Hex() + "/down": sshDown,
	} {
		id, dir, ok := ParseSSHData(suffix)
		if !ok || id != sid || dir != want {
			t.Errorf("ParseSSHData(%q) = %s %q %v", suffix, id.Hex(), dir, ok)
		}
	}
	for _, suffix := range []string{
		"ssh/session", "ssh/+/up", "ssh/+/down", "ssh/#", "ssh/" + sid.Hex(), "ssh/" + sid.Hex() + "/other",
		"ssh/" + sid.Hex() + "/down/x", "ssh/nothex/down", "ssh//down", "ssh/" + strings.ToUpper(sid.Hex()) + "x/up",
		"logs/" + sid.Hex() + "/down",
	} {
		if _, _, ok := ParseSSHData(suffix); ok {
			t.Errorf("ParseSSHData(%q) accepted", suffix)
		}
	}

	down, up := "ssh/"+sid.Hex()+"/down", "ssh/"+sid.Hex()+"/up"
	for name, got := range map[string]bool{
		"device publishes down":             DeviceMayPublish(down),
		"device subscribes to ssh/session":  DeviceMaySubscribe(SuffixSSHSession),
		"device subscribes to ssh/+/up":     DeviceMaySubscribe("ssh/+/up"),
		"device is delivered its up frames": DeviceMaySubscribe(up),
	} {
		if !got {
			t.Errorf("refused: %s", name)
		}
	}
	for name, got := range map[string]bool{
		"device publishes up":             DeviceMayPublish(up),
		"device publishes ssh/session":    DeviceMayPublish(SuffixSSHSession),
		"device publishes ssh/+/down":     DeviceMayPublish("ssh/+/down"),
		"device subscribes to down":       DeviceMaySubscribe(down),
		"device subscribes to ssh/+/+":    DeviceMaySubscribe("ssh/+/+"),
		"device subscribes to ssh/#":      DeviceMaySubscribe("ssh/#"),
		"device subscribes to ssh/+/down": DeviceMaySubscribe("ssh/+/down"),
	} {
		if got {
			t.Errorf("allowed: %s", name)
		}
	}
	for _, suffix := range []string{"ssh", "ssh/session", "ssh/#", "ssh/+", "ssh/+/up", "ssh/+/down", up, down} {
		if UserMaySubscribe(suffix) {
			t.Errorf("UserMaySubscribe(%q)", suffix)
		}
	}
	if !UserMaySubscribe("sshx") || !UserMaySubscribe(SuffixStatus) {
		t.Error("a user lost a topic that merely looks like ssh")
	}
}

func insertSSHSession(t *testing.T, client *mongo.Client, deviceID primitive.ObjectID, mutate func(*devices.SSHSession)) devices.SSHSession {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	session := devices.SSHSession{
		ID: primitive.NewObjectID(), DeviceID: deviceID, Owner: testAlice, CreatedBy: testAlice,
		Target: "_pv_", Pubkey: testSSHPubkey,
		CreatedAt: now, ExpiresAt: now.Add(devices.SSHSessionLease), Deadline: now.Add(devices.SSHSessionMaxDuration),
		Live: true, ActiveAt: now, DownWindowAt: now,
	}
	if mutate != nil {
		mutate(&session)
	}
	if _, err := client.Database(utils.MongoDb).Collection(devices.SSHSessionsCollection).InsertOne(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func loadSSHSession(t *testing.T, client *mongo.Client, id primitive.ObjectID) devices.SSHSession {
	t.Helper()
	session := devices.SSHSession{}
	err := client.Database(utils.MongoDb).Collection(devices.SSHSessionsCollection).
		FindOne(context.Background(), bson.M{"_id": id}).Decode(&session)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func countSSHFrames(t *testing.T, client *mongo.Client, session primitive.ObjectID, dir string) int64 {
	t.Helper()
	n, err := client.Database(utils.MongoDb).Collection(devices.SSHFramesCollection).
		CountDocuments(context.Background(), bson.M{"session": session, "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func sshFramePayload(seq int, data string, end bool, reason string) string {
	raw, _ := json.Marshal(map[string]interface{}{"seq": seq, "data": []byte(data), "end": end, "reason": reason})
	return string(raw)
}

// A device publishes on the down topic of any session id under its own
// namespace, with no lookup: whether the session takes the frame is the
// bridge's to decide (it drops the frame), because a refused QoS 1 publish
// disconnects an MQTT 3.1.1 device. It subscribes to its own ssh/session and
// ssh/+/up. Users do neither, whatever the filter.
func TestSSHACL(t *testing.T) {
	h := &authHook{} // no mongo client: a lookup would panic
	own := primitive.NewObjectID().Hex()
	other := primitive.NewObjectID().Hex()
	session := primitive.NewObjectID()

	cl := &mochi.Client{}
	setIdentity(cl, kindDevice, own, "")

	for _, sid := range []primitive.ObjectID{session, primitive.NewObjectID(), primitive.NilObjectID} {
		if !h.OnACLCheck(cl, downTopic(own, sid), true) {
			t.Errorf("a device may not publish on its own down topic of session %s", sid.Hex())
		}
	}
	for name, allowed := range map[string]bool{
		"publish on another device's topic":       h.OnACLCheck(cl, downTopic(other, session), true),
		"publish on a malformed session id":       h.OnACLCheck(cl, Prefix+own+"/ssh/nothex/down", true),
		"publish on ssh/+/down":                   h.OnACLCheck(cl, Prefix+own+"/ssh/+/down", true),
		"publish on its up topic":                 h.OnACLCheck(cl, sshUpTopic(own, session), true),
		"publish on ssh/session":                  h.OnACLCheck(cl, Topic(own, SuffixSSHSession), true),
		"subscribe to its down topic":             h.OnACLCheck(cl, downTopic(own, session), false),
		"subscribe to another's ssh/session":      h.OnACLCheck(cl, Topic(other, SuffixSSHSession), false),
		"subscribe to another's up frames":        h.OnACLCheck(cl, Prefix+other+"/ssh/+/up", false),
		"subscribe to ssh/#":                      h.OnACLCheck(cl, Prefix+own+"/ssh/#", false),
		"subscribe to every device's ssh/session": h.OnACLCheck(cl, Prefix+"+/"+SuffixSSHSession, false),
	} {
		if allowed {
			t.Errorf("a device may %s", name)
		}
	}
	for _, filter := range []string{Topic(own, SuffixSSHSession), Prefix + own + "/ssh/+/up", sshUpTopic(own, session)} {
		if !h.OnACLCheck(cl, filter, false) {
			t.Errorf("a device may not subscribe to %s", filter)
		}
	}

	if h.OnACLCheck(claimWaitClient(own), downTopic(own, session), true) ||
		h.OnACLCheck(claimWaitClient(own), Topic(own, SuffixSSHSession), false) {
		t.Error("a claim-wait session may use the SSH topics")
	}

	for _, scope := range []string{scopeAll, scopeReadOnly} {
		user := &mochi.Client{}
		setIdentity(user, kindUser, testAlice, scope)
		cacheOwnership(user, own, true)
		for _, topic := range []string{Topic(own, SuffixSSHSession), downTopic(own, session), sshUpTopic(own, session),
			Prefix + own + "/ssh/+/up", Prefix + own + "/ssh/+/down", Prefix + own + "/ssh/#"} {
			if h.OnACLCheck(user, topic, false) {
				t.Errorf("a user (%s) may subscribe to %s", scope, topic)
			}
			if h.OnACLCheck(user, topic, true) {
				t.Errorf("a user (%s) may publish on %s", scope, topic)
			}
		}
		// Wildcards over the owned device are granted at SUBSCRIBE; every
		// delivery is checked again with the concrete topic, refused above
		// (TestUserWildcardsNeverDeliverSSH).
		for _, filter := range []string{"/#", "/+/session", "/+/+/up", "/+/+/down"} {
			if !h.OnACLCheck(user, Prefix+own+filter, false) {
				t.Errorf("a user (%s) lost its owned device's %s", scope, filter)
			}
		}
	}
}

// A device on MQTT 3.1.1 that sends QoS 1 frames for a session that is over,
// unknown or another device's gets its PUBACK and stays connected: the frames
// are dropped (and counted) by the bridge, never stored.
func TestLateSSHFramesKeepTheDeviceConnected(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 0)
	device := insertDevice(t, client, testAlice, nil)
	otherDevice := insertDevice(t, client, testAlice, nil)

	endedAt := time.Now().UTC().Add(-time.Minute)
	ended := insertSSHSession(t, client, device, func(s *devices.SSHSession) {
		s.Live, s.EndedAt, s.EndedBy, s.Reason = false, &endedAt, devices.SSHEndedByHub, devices.SSHEndStopped
	})
	others := insertSSHSession(t, client, otherDevice, nil)

	c, code := dialWire(t, b.server, 4, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("CONNACK %d", code)
	}
	for i, sid := range []primitive.ObjectID{ended.ID, primitive.NewObjectID(), others.ID} {
		id := uint16(i + 1)
		err := c.send(t, packets.Packet{
			FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: 1},
			TopicName:   downTopic(device.Hex(), sid), PacketID: id,
			Payload: []byte(sshFramePayload(3, "late output", false, "")),
		})
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		pk, ok := c.next(5 * time.Second)
		if !ok {
			t.Fatalf("frame %d: the device was disconnected", i)
		}
		if pk.FixedHeader.Type != packets.Puback {
			t.Fatalf("frame %d: expected PUBACK, got packet type %d", i, pk.FixedHeader.Type)
		}
		if n := countSSHFrames(t, client, sid, devices.SSHDirDown); n != 0 {
			t.Errorf("frame %d: stored %d frames", i, n)
		}
	}
	if cl, ok := b.server.Clients.Get(device.Hex()); !ok || cl.Closed() {
		t.Error("the device is no longer connected")
	}
}

// A device's CONNECT may not arm a will on its SSH topics.
func TestSSHWillRefused(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 0)
	device := insertDevice(t, client, testAlice, nil)
	session := insertSSHSession(t, client, device, nil)

	params := connectParams(device)
	params.WillFlag, params.WillTopic, params.WillPayload = true, downTopic(device.Hex(), session.ID), []byte(sshFramePayload(9, "", true, "closed"))
	if _, code := dialWire(t, b.server, 5, params); code == packets.CodeSuccess.Code {
		t.Error("a will on an SSH down topic was armed")
	}
}

// Defence in depth: were the ACL to let them through, the bridge still
// refuses an SSH session message or up frame from anyone but the Hub, a
// retained frame from anyone, and a QoS 2 frame.
func TestBridgeSSHRefusals(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	session := primitive.NewObjectID()
	h := newTestBridge() // no mongo client: reaching a write would panic

	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")
	user := &mochi.Client{}
	setIdentity(user, kindUser, testAlice, scopeAll)
	inline := &mochi.Client{}
	inline.Net.Inline = true

	publish := func(cl *mochi.Client, topic string, qos byte, retain bool, payload string) error {
		pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: qos, Retain: retain},
			TopicName: topic, Payload: []byte(payload)}
		_, err := h.OnPublish(cl, pk)
		return err
	}

	for name, cl := range map[string]*mochi.Client{"device": device, "user": user, "claim-wait": claimWaitClient(deviceID), "anonymous": {}} {
		if err := publish(cl, Topic(deviceID, SuffixSSHSession), 1, false, `{"action":"start"}`); !errors.Is(err, packets.ErrRejectPacket) {
			t.Errorf("%s published an SSH session message", name)
		}
		if err := publish(cl, sshUpTopic(deviceID, session), 1, false, sshFramePayload(0, "x", false, "")); !errors.Is(err, packets.ErrRejectPacket) {
			t.Errorf("%s published an up frame", name)
		}
	}
	if err := publish(inline, Topic(deviceID, SuffixSSHSession), 1, false, `{}`); err != nil {
		t.Errorf("the Hub's SSH session message refused: %v", err)
	}
	if err := publish(inline, sshUpTopic(deviceID, session), 1, false, sshFramePayload(0, "x", false, "")); err != nil {
		t.Errorf("the Hub's up frame refused: %v", err)
	}
	for _, topic := range []string{Topic(deviceID, SuffixSSHSession), sshUpTopic(deviceID, session)} {
		if err := publish(inline, topic, 1, true, `{}`); !errors.Is(err, packets.ErrRejectPacket) {
			t.Errorf("the Hub's %s was retained", topic)
		}
	}

	frame := sshFramePayload(0, "", false, "")
	if err := publish(device, downTopic(deviceID, session), 1, true, frame); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("a retained frame was accepted")
	}
	if err := publish(device, downTopic(deviceID, session), 2, false, frame); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("a QoS 2 frame was accepted")
	}
	// A malformed frame is dropped, not refused, and never fanned out.
	for _, bad := range []string{`not json`, `{"data":"aGk="}`, `{"seq":0,"data":"` + strings.Repeat("A", devices.MaxSSHFramePayload) + `"}`} {
		if err := publish(device, downTopic(deviceID, session), 1, false, bad); !errors.Is(err, packets.CodeSuccessIgnore) {
			t.Errorf("a malformed frame was not dropped: %v", err)
		}
	}
	if len(h.jobs) != 0 {
		t.Errorf("%d writes queued for SSH frames", len(h.jobs))
	}
}

// The bridge stores a frame only for a session of the publishing device that
// takes it, ends the session on its last frame, and never fans a frame out.
func TestIngestSSHFrames(t *testing.T) {
	client := newTestMongo(t)
	h := newTestBridge()
	h.mongoClient = client

	device := primitive.NewObjectID()
	other := primitive.NewObjectID()
	cl := &mochi.Client{}
	setIdentity(cl, kindDevice, device.Hex(), "")

	publish := func(topicDevice primitive.ObjectID, session primitive.ObjectID, payload string) error {
		pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: 1},
			TopicName: downTopic(topicDevice.Hex(), session), Payload: []byte(payload)}
		_, err := h.OnPublish(cl, pk)
		switch {
		case err == nil:
			return errors.New("frame not ignored by the broker")
		case errors.Is(err, packets.CodeSuccessIgnore):
			return nil
		}
		return err
	}

	live := insertSSHSession(t, client, device, nil)
	if err := publish(device, live.ID, sshFramePayload(0, "", false, "")); err != nil {
		t.Fatal(err)
	}
	if err := publish(device, live.ID, sshFramePayload(1, "SSH-2.0-dropbear\r\n", false, "")); err != nil {
		t.Fatal(err)
	}
	if n := countSSHFrames(t, client, live.ID, devices.SSHDirDown); n != 2 {
		t.Fatalf("%d frames stored right after the publishes, want 2", n)
	}
	if got := loadSSHSession(t, client, live.ID); got.DownBytes != int64(len("SSH-2.0-dropbear\r\n")) || got.DownFrames != 2 {
		t.Errorf("counted %d bytes in %d frames", got.DownBytes, got.DownFrames)
	}

	othersSession := insertSSHSession(t, client, other, nil)
	if err := publish(device, othersSession.ID, sshFramePayload(0, "x", false, "")); err != nil {
		t.Errorf("a frame for another device's session was refused rather than dropped: %v", err)
	}
	if n := countSSHFrames(t, client, othersSession.ID, devices.SSHDirDown); n != 0 {
		t.Error("a frame for another device's session was stored")
	}
	if err := publish(other, othersSession.ID, sshFramePayload(0, "x", false, "")); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("a device published on another device's topic")
	}

	if err := publish(device, live.ID, sshFramePayload(2, "", true, "closed")); err != nil {
		t.Fatal(err)
	}
	got := loadSSHSession(t, client, live.ID)
	if got.Live || got.Reason != "closed" || got.EndedBy != devices.SSHEndedByDevice || !got.EndAudited {
		t.Errorf("after the last frame: %+v", got)
	}
	if len(h.jobs) != 0 {
		t.Errorf("%d writes queued by SSH frames", len(h.jobs))
	}
}

func TestSSHSessionMessage(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 9, 0, 0, time.UTC)
	session := devices.SSHSession{
		ID: primitive.NewObjectID(), DeviceID: primitive.NewObjectID(), Target: "_pv_", Pubkey: testSSHPubkey,
		Live: true, ExpiresAt: now.Add(time.Minute), Deadline: now.Add(time.Hour),
	}
	topic, payload, ok := sshSessionMessage(&session, logActionStart, now)
	if !ok {
		t.Fatal("start not sent")
	}
	if topic != "ph/v1/dev/"+session.DeviceID.Hex()+"/ssh/session" {
		t.Errorf("topic = %q", topic)
	}
	want := `{"id":"` + session.ID.Hex() + `","action":"start","target":"_pv_","pubkey":"` + testSSHPubkey + `",` +
		`"expires_at":"2026-09-28T15:10:00Z","deadline":"2026-09-28T16:09:00Z"}`
	if string(payload) != want {
		t.Errorf("payload = %s\nwant      %s", payload, want)
	}
	if _, _, ok := sshSessionMessage(&session, logActionStart, now.Add(time.Minute)); ok {
		t.Error("a start past the lease was sent")
	}
	if _, _, ok := sshSessionMessage(&session, logActionStop, now.Add(2*time.Hour)); !ok {
		t.Error("a stop past the lease was not sent")
	}
	noDevice := session
	noDevice.DeviceID = primitive.NilObjectID
	if _, _, ok := sshSessionMessage(&noDevice, logActionStart, now); ok {
		t.Error("a session without a device was sent")
	}

	updated := func(fields bson.M) *changeEvent {
		raw, _ := bson.Marshal(fields)
		event := &changeEvent{OperationType: "update"}
		event.UpdateDescription.UpdatedFields = raw
		return event
	}
	live := &devices.SSHSession{Live: true}
	byHub := &devices.SSHSession{Live: false, EndedBy: devices.SSHEndedByHub}
	byDevice := &devices.SSHSession{Live: false, EndedBy: devices.SSHEndedByDevice}
	for _, tc := range []struct {
		name    string
		event   *changeEvent
		session *devices.SSHSession
		want    string
	}{
		{"insert", &changeEvent{OperationType: "insert"}, live, logActionStart},
		{"renew", updated(bson.M{"expires_at": time.Now()}), live, logActionRenew},
		{"stop", updated(bson.M{"live": false}), byHub, logActionStop},
		{"ended by the device", updated(bson.M{"live": false}), byDevice, ""},
		{"counters", updated(bson.M{"down_bytes": 3, "active_at": time.Now()}), live, ""},
		{"attached", updated(bson.M{"attached": true}), live, ""},
	} {
		action, ok := sshSessionAction(tc.event, tc.session)
		if (tc.want == "") == ok || (ok && action != tc.want) {
			t.Errorf("%s: action %q, ok %v, want %q", tc.name, action, ok, tc.want)
		}
	}
}

// The SSH change streams carry only what the notifier acts on: frames and
// the relay's bookkeeping wake no replica.
func TestSSHPipelines(t *testing.T) {
	client := newTestMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := client.Database(utils.MongoDb)
	session := insertSSHSession(t, client, primitive.NewObjectID(), nil)
	frames := db.Collection(devices.SSHFramesCollection)
	if _, err := frames.InsertOne(ctx, bson.M{"probe": true}); err != nil {
		t.Fatal(err)
	}

	sessionStream, err := db.Collection(devices.SSHSessionsCollection).Watch(ctx, sshSessionPipeline(), options.ChangeStream().SetFullDocument(options.UpdateLookup))
	if err != nil {
		t.Fatal(err)
	}
	defer sessionStream.Close(context.Background())
	frameStream, err := frames.Watch(ctx, sshUpFramePipeline())
	if err != nil {
		t.Fatal(err)
	}
	defer frameStream.Close(context.Background())

	sessions := db.Collection(devices.SSHSessionsCollection)
	for _, set := range []bson.M{
		{"$inc": bson.M{"down_bytes": 10, "down_frames": 1}},
		{"$set": bson.M{"active_at": time.Now(), "attached": true}},
		{"$set": bson.M{"end_audited": true}},
		{"$set": bson.M{"expires_at": session.ExpiresAt.Add(20 * time.Second)}},
	} {
		if _, err := sessions.UpdateOne(ctx, bson.M{"_id": session.ID}, set); err != nil {
			t.Fatal(err)
		}
	}
	if !sessionStream.Next(ctx) {
		t.Fatal(sessionStream.Err())
	}
	event := changeEvent{}
	if err := sessionStream.Decode(&event); err != nil || !updatedField(&event, "expires_at") {
		t.Fatalf("first session event: %s (%v), want the renew", event.UpdateDescription.UpdatedFields, err)
	}

	for _, dir := range []string{devices.SSHDirDown, devices.SSHDirDown, devices.SSHDirUp} {
		if _, err := frames.InsertOne(ctx, bson.M{"_id": primitive.NewObjectID(), "session": session.ID, "dir": dir}); err != nil {
			t.Fatal(err)
		}
	}
	if !frameStream.Next(ctx) {
		t.Fatal(frameStream.Err())
	}
	event = changeEvent{}
	if err := frameStream.Decode(&event); err != nil {
		t.Fatal(err)
	}
	if dir, _ := event.FullDocument.Lookup("dir").StringValueOK(); dir != devices.SSHDirUp {
		t.Fatalf("first frame event: %s, want the up frame", event.FullDocument)
	}
}

// A user's wildcard subscription over its own device never delivers an SSH
// topic: down frames are never fanned out, and ssh/session and up frames,
// which the device must keep receiving, are withheld from users by the ACL
// the broker runs again on every delivery.
func TestUserWildcardsNeverDeliverSSH(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 0)

	device := insertDevice(t, client, testAlice, nil)
	own := device.Hex()
	session := insertSSHSession(t, client, device, nil)

	dev, code := dialWire(t, b.server, 4, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("device CONNECT refused: %#x", code)
	}
	if codes := dev.subscribe(t, Prefix+own+"/ssh/+/up", Topic(own, SuffixSSHSession)); len(codes) != 2 || !granted(codes[0]) || !granted(codes[1]) {
		t.Fatalf("device refused its SSH topics: %v", codes)
	}

	filters := []string{Prefix + own + "/#", Prefix + own + "/+/session", Prefix + own + "/+/+/up", Prefix + own + "/+/+/down", Prefix + own + "/+"}
	for _, version := range []byte{4, 5} {
		user, code := dialWire(t, b.server, version, packets.ConnectParams{
			Clean: true, Keepalive: 60, ClientIdentifier: "dashboard-alice-" + strconv.Itoa(int(version)),
			Username: []byte(devicePrnPrefix + own),
			Password: []byte(signToken(t, jwtgo.MapClaims{"type": "USER", "prn": testAlice, "scopes": scopeAll})),
		})
		if code != packets.CodeSuccess.Code {
			t.Fatalf("user CONNECT refused: %#x", code)
		}
		if codes := user.subscribe(t, filters...); len(codes) != len(filters) || !granted(codes[0]) {
			t.Fatalf("v%d: the owner lost its device's %s: %v", version, filters[0], codes)
		}

		seq := int(version)
		dev.publish(t, downTopic(own, session.ID), sshFramePayload(seq, "secret", false, ""), 1, false)
		if ack, ok := dev.next(5 * time.Second); !ok || ack.FixedHeader.Type != packets.Puback {
			t.Fatalf("v%d: no PUBACK for a stored frame: %+v", version, ack.FixedHeader)
		}
		if err := b.server.Publish(Topic(own, SuffixSSHSession), []byte(`{"id":"`+session.ID.Hex()+`","action":"renew"}`), false, 1); err != nil {
			t.Fatal(err)
		}
		if err := b.server.Publish(sshUpTopic(own, session.ID), []byte(sshFramePayload(seq, "typed", false, "")), false, 1); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{Topic(own, SuffixSSHSession), sshUpTopic(own, session.ID)} {
			if pk := dev.nextPublish(t, 5*time.Second); pk.TopicName != want {
				t.Fatalf("device got %q, want %q", pk.TopicName, want)
			}
		}
		dev.publish(t, Topic(own, SuffixStatus), `{"status":"online"}`, 0, false)

		pk := user.nextPublish(t, 5*time.Second)
		if pk.TopicName != Topic(own, SuffixStatus) {
			t.Fatalf("v%d: user received %q (%s)", version, pk.TopicName, pk.Payload)
		}
		user.expectSilence(t, 200*time.Millisecond)
	}
	if n := countSSHFrames(t, client, session.ID, devices.SSHDirDown); n != 2 {
		t.Errorf("%d frames stored, want 2", n)
	}
}

// ack acknowledges a QoS 1 PUBLISH from the broker.
func (c *wireClient) ack(t *testing.T, pk packets.Packet) {
	t.Helper()
	if pk.FixedHeader.Qos > 0 {
		c.send(t, packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Puback}, PacketID: pk.PacketID})
	}
}

// publishAcked publishes at QoS 1 and waits for the PUBACK, keeping the
// PUBLISHes that arrive meanwhile.
func (c *wireClient) publishAcked(t *testing.T, topic, payload string) {
	t.Helper()
	c.publish(t, topic, payload, 1, false)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case pk, ok := <-c.in:
			if !ok {
				t.Fatal("closed before the PUBACK")
			}
			if pk.FixedHeader.Type == packets.Puback {
				return
			}
			c.pending = append(c.pending, pk)
		case <-deadline:
			t.Fatal("no PUBACK")
		}
	}
}

func (c *wireClient) nextFrame(t *testing.T, topic string) (sshFrame struct {
	Seq    int64  `json:"seq"`
	Data   []byte `json:"data"`
	End    bool   `json:"end"`
	Reason string `json:"reason"`
}) {
	t.Helper()
	pk := c.nextPublish(t, 10*time.Second)
	c.ack(t, pk)
	if pk.TopicName != topic {
		t.Fatalf("got %q (%s), want %q", pk.TopicName, pk.Payload, topic)
	}
	if pk.FixedHeader.Retain || pk.FixedHeader.Qos != 1 {
		t.Errorf("up frame retained %v at QoS %d", pk.FixedHeader.Retain, pk.FixedHeader.Qos)
	}
	if err := json.Unmarshal(pk.Payload, &sshFrame); err != nil {
		t.Fatalf("payload %s: %v", pk.Payload, err)
	}
	return sshFrame
}

// End to end, over two replicas: the browser's WebSocket and a device on
// replica B, both notifiers running. The start reaches the device; the
// browser's first bytes wait for the device's ready frame; bytes flow both
// ways in order, split to 32 KiB; each up frame reaches the device once; and
// closing the WebSocket stops the session on the device.
func TestSSHRelayEndToEnd(t *testing.T) {
	client := newTestMongo(t)
	a := newClaimBroker(t, client, "A", 0)
	b := newClaimBroker(t, client, "B", 0)

	device := insertDevice(t, client, testAlice, nil)
	own := device.Hex()
	onB, code := dialWire(t, b.server, 4, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("device CONNECT refused: %#x", code)
	}
	// As the agent does: ssh/+/up first, then ssh/session.
	if codes := onB.subscribe(t, Prefix+own+"/ssh/+/up"); !granted(codes[0]) {
		t.Fatalf("device refused ssh/+/up: %v", codes)
	}
	if codes := onB.subscribe(t, Topic(own, SuffixSSHSession)); !granted(codes[0]) {
		t.Fatalf("device refused ssh/session: %v", codes)
	}
	if !b.notifier.holdsDevice(own) || a.notifier.holdsDevice(own) {
		t.Fatal("holdsDevice does not name B alone")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.notifier.Run(ctx)
	go b.notifier.Run(ctx)

	// Both streams deliver: a probe session's up frame reaches the device.
	probe := insertSSHSession(t, client, device, func(s *devices.SSHSession) { s.Live = false })
	framesColl := client.Database(utils.MongoDb).Collection(devices.SSHFramesCollection)
	deadline := time.Now().Add(30 * time.Second)
	for seq := 0; ; seq++ {
		if _, err := framesColl.InsertOne(ctx, devices.SSHFrame{ID: primitive.NewObjectID(), Session: probe.ID, DeviceID: device,
			Dir: devices.SSHDirUp, Seq: int64(seq), Data: []byte{}, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if pk, ok := onB.next(300 * time.Millisecond); ok {
			onB.ack(t, pk)
			if pk.TopicName == sshUpTopic(own, probe.ID) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the frames change stream never opened")
		}
	}
	for { // drain the probes still in flight
		pk, ok := onB.next(500 * time.Millisecond)
		if !ok {
			break
		}
		onB.ack(t, pk)
	}
	waitSSHSessionStream(t, client, onB, own)

	session := insertSSHSession(t, client, device, func(s *devices.SSHSession) { s.DeviceSlot = 1 })
	start := onB.nextPublish(t, 10*time.Second)
	onB.ack(t, start)
	notice := sshSessionNotice{}
	if err := json.Unmarshal(start.Payload, &notice); err != nil || start.TopicName != Topic(own, SuffixSSHSession) ||
		notice.Action != logActionStart || notice.ID != session.ID.Hex() || notice.Pubkey != testSSHPubkey {
		t.Fatalf("start: %s on %s (%v)", start.Payload, start.TopicName, err)
	}

	// The browser, served by any replica's API: a ticket first (as the
	// session's owner), then the WebSocket with the ticket as its
	// subprotocol.
	app := devices.Build(client, nil)
	e := echo.New()
	e.POST("/devices/:id/ssh-sessions/:sid/ticket", app.HandleSSHSessionTicket, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Set(echoutil.KeyJWTPayload, jwtgo.MapClaims{"prn": testAlice, "type": "USER"})
			return next(c)
		}
	})
	e.GET("/devices/:id/ssh-sessions/:sid/ws", app.HandleSSHWebSocket, echoutil.WebSocketTicket())
	server := httptest.NewServer(e)
	defer server.Close()
	sessionURL := server.URL + "/devices/" + own + "/ssh-sessions/" + session.ID.Hex()
	resp, err := http.Post(sessionURL+"/ticket", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	ticket := devices.SSHSessionTicket{}
	err = json.NewDecoder(resp.Body).Decode(&ticket)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusCreated || ticket.Ticket == "" {
		t.Fatalf("ticket: status %d (%v)", resp.StatusCode, err)
	}
	dialer := websocket.Dialer{Subprotocols: []string{"ticket." + ticket.Ticket}}
	ws, wsResp, err := dialer.Dial("ws"+strings.TrimPrefix(sessionURL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if got := wsResp.Header.Get("Sec-WebSocket-Protocol"); got != "ticket."+ticket.Ticket {
		t.Fatalf("subprotocol echoed: %q", got)
	}

	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("SSH-2.0-browser\r\n")); err != nil {
		t.Fatal(err)
	}
	onB.expectSilence(t, 500*time.Millisecond)

	onB.publishAcked(t, downTopic(own, session.ID), sshFramePayload(0, "", false, ""))
	up := sshUpTopic(own, session.ID)
	if frame := onB.nextFrame(t, up); frame.Seq != 0 || string(frame.Data) != "SSH-2.0-browser\r\n" {
		t.Fatalf("first up frame: %+v", frame)
	}

	onB.publishAcked(t, downTopic(own, session.ID), sshFramePayload(2, "world", false, ""))
	onB.publishAcked(t, downTopic(own, session.ID), sshFramePayload(1, "hello ", false, ""))
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	for _, want := range []string{"hello ", "world"} {
		_, data, err := ws.ReadMessage()
		if err != nil || string(data) != want {
			t.Fatalf("browser read %q (%v), want %q", data, err, want)
		}
	}

	big := bytes.Repeat([]byte("k"), devices.MaxSSHFrameData+100)
	if err := ws.WriteMessage(websocket.BinaryMessage, big); err != nil {
		t.Fatal(err)
	}
	if frame := onB.nextFrame(t, up); frame.Seq != 1 || len(frame.Data) != devices.MaxSSHFrameData {
		t.Fatalf("second up frame: seq %d, %d bytes", frame.Seq, len(frame.Data))
	}
	if frame := onB.nextFrame(t, up); frame.Seq != 2 || len(frame.Data) != 100 {
		t.Fatalf("third up frame: seq %d, %d bytes", frame.Seq, len(frame.Data))
	}

	// Forwarded frames leave the store while the session is live: the up
	// frames once B published them, the down frames once the WebSocket
	// wrote them.
	deadline = time.Now().Add(10 * time.Second)
	for countSSHFrames(t, client, session.ID, devices.SSHDirUp) != 0 || countSSHFrames(t, client, session.ID, devices.SSHDirDown) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("frames left after forwarding: %d up, %d down",
				countSSHFrames(t, client, session.ID, devices.SSHDirUp), countSSHFrames(t, client, session.ID, devices.SSHDirDown))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Closing the WebSocket stops the session on the device; no up frame
	// came twice.
	if err := ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatal(err)
	}
	stop := onB.nextPublish(t, 10*time.Second)
	onB.ack(t, stop)
	if err := json.Unmarshal(stop.Payload, &notice); err != nil || stop.TopicName != Topic(own, SuffixSSHSession) || notice.Action != logActionStop {
		t.Fatalf("stop: %s on %s", stop.Payload, stop.TopicName)
	}
	onB.expectSilence(t, 500*time.Millisecond)
	got := loadSSHSession(t, client, session.ID)
	if got.Live || got.Reason != devices.SSHEndStopped || got.UpBytes != int64(len("SSH-2.0-browser\r\n")+len(big)) ||
		got.DownBytes != int64(len("hello world")) || !got.EndAudited {
		t.Errorf("session after the close: %+v", got)
	}

	// The device's late last frame is still taken (grace); later ones, and
	// frames of a session long over, are dropped, and the device stays
	// connected.
	onB.publishAcked(t, downTopic(own, session.ID), sshFramePayload(3, "", true, devices.SSHEndStopped))
	other := insertSSHSession(t, client, device, func(s *devices.SSHSession) {
		endedAt := time.Now().UTC().Add(-time.Hour)
		s.Live, s.EndedAt = false, &endedAt
	})
	for { // the start of `other` is not sent (not live); drain anything else
		pk, ok := onB.next(300 * time.Millisecond)
		if !ok {
			break
		}
		onB.ack(t, pk)
	}
	onB.publishAcked(t, downTopic(own, other.ID), sshFramePayload(0, "late", false, ""))
	if n := countSSHFrames(t, client, other.ID, devices.SSHDirDown); n != 0 {
		t.Errorf("a frame of a session long over was stored (%d)", n)
	}
	if !b.notifier.holdsDevice(own) {
		t.Error("a late frame disconnected the device")
	}
}

// waitSSHSessionStream returns once the SSH sessions change stream delivers:
// a probe session's start reaches the device.
func waitSSHSessionStream(t *testing.T, client *mongo.Client, dev *wireClient, own string) {
	t.Helper()
	device, _ := primitive.ObjectIDFromHex(own)
	sessions := client.Database(utils.MongoDb).Collection(devices.SSHSessionsCollection)
	deadline := time.Now().Add(30 * time.Second)
	for {
		probe := insertSSHSession(t, client, device, nil)
		pk, ok := dev.next(300 * time.Millisecond)
		// Free the slot for the next probe or the test's own session.
		if _, err := sessions.DeleteOne(context.Background(), bson.M{"_id": probe.ID}); err != nil {
			t.Fatal(err)
		}
		if ok {
			dev.ack(t, pk)
			if pk.TopicName == Topic(own, SuffixSSHSession) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the SSH sessions change stream never opened")
		}
	}
	for { // drain the other probes' starts
		pk, ok := dev.next(500 * time.Millisecond)
		if !ok {
			return
		}
		dev.ack(t, pk)
	}
}
