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
	"strconv"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// A device subscribes to its own logs/session and publishes on its own
// logs/stream, and nothing else of the live log topics; users do neither.
// None of it reaches the database.
func TestLogSessionACL(t *testing.T) {
	h := &authHook{} // no mongo client: userOwns would deny, not allow
	own := "5f0000000000000000000001"
	other := "5f0000000000000000000002"

	device := &mochi.Client{}
	setIdentity(device, kindDevice, own, "")

	if !h.OnACLCheck(device, Topic(own, SuffixLogSession), false) {
		t.Error("a device may not subscribe to its own log sessions")
	}
	if !h.OnACLCheck(device, Topic(own, SuffixLogStream), true) {
		t.Error("a device may not stream its own logs")
	}
	for name, allowed := range map[string]bool{
		"publish its own logs/session":        h.OnACLCheck(device, Topic(own, SuffixLogSession), true),
		"subscribe to its own logs/stream":    h.OnACLCheck(device, Topic(own, SuffixLogStream), false),
		"subscribe to another's logs/session": h.OnACLCheck(device, Topic(other, SuffixLogSession), false),
		"publish another's logs/stream":       h.OnACLCheck(device, Topic(other, SuffixLogStream), true),
		"publish another's logs/session":      h.OnACLCheck(device, Topic(other, SuffixLogSession), true),
		"subscribe to logs/#":                 h.OnACLCheck(device, Prefix+own+"/logs/#", false),
		"subscribe to logs/+":                 h.OnACLCheck(device, Prefix+own+"/logs/+", false),
		"subscribe to every logs/session":     h.OnACLCheck(device, Prefix+"+/"+SuffixLogSession, false),
	} {
		if allowed {
			t.Errorf("a device may %s", name)
		}
	}

	if h.OnACLCheck(claimWaitClient(own), Topic(own, SuffixLogSession), false) ||
		h.OnACLCheck(claimWaitClient(own), Topic(own, SuffixLogStream), true) {
		t.Error("a claim-wait session may use the live log topics")
	}

	for _, scope := range []string{scopeAll, scopeReadOnly} {
		user := &mochi.Client{}
		setIdentity(user, kindUser, testAlice, scope)
		// Cached as owned, to prove the refusal does not rest on ownership.
		cacheOwnership(user, own, true)
		for _, suffix := range []string{SuffixLogSession, SuffixLogStream} {
			if h.OnACLCheck(user, Topic(own, suffix), false) {
				t.Errorf("a user (%s) may subscribe to %s", scope, suffix)
			}
			if h.OnACLCheck(user, Topic(own, suffix), true) {
				t.Errorf("a user (%s) may publish on %s", scope, suffix)
			}
		}
		if !h.OnACLCheck(user, Topic(own, SuffixLogs), false) {
			t.Errorf("a user (%s) lost its owned device's persisted logs topic", scope)
		}

		// A wildcard filter over the owned device is granted: it is how a
		// client watches everything a user may read (status, device-meta,
		// ...). It still never delivers a live log topic, because mochi
		// runs this same check again on every delivery, with the concrete
		// topic, and that is refused above (the broker-level proof is
		// TestUserWildcardsNeverDeliverLiveLogs).
		for _, filter := range []string{"/#", "/logs/#", "/logs/+", "/+/stream", "/+/session", "/+"} {
			if !h.OnACLCheck(user, Prefix+own+filter, false) {
				t.Errorf("a user (%s) lost its owned device's %s", scope, filter)
			}
			if h.OnACLCheck(user, Prefix+own+filter, true) {
				t.Errorf("a user (%s) may publish on %s", scope, filter)
			}
		}
		// A wildcard in the device id owns nothing.
		for _, filter := range []string{"+/" + SuffixLogStream, "+/#", "#"} {
			if h.OnACLCheck(user, Prefix+filter, false) {
				t.Errorf("a user (%s) may subscribe to %s", scope, Prefix+filter)
			}
		}
		for _, suffix := range []string{SuffixClaimed, SuffixLogSession, SuffixLogStream} {
			if h.OnACLCheck(user, Topic(own, suffix), false) {
				t.Errorf("a user (%s) may be delivered %s", scope, suffix)
			}
		}
	}
}

// Defence in depth: were the ACL to let them through, the bridge still
// refuses a log session message from anyone but the Hub, a retained one from
// anyone, and a retained or QoS 2 batch.
func TestBridgeLogTopicRefusals(t *testing.T) {
	deviceID := primitive.NewObjectID().Hex()
	h := newTestBridge() // no mongo client: reaching a write would panic

	device := &mochi.Client{}
	setIdentity(device, kindDevice, deviceID, "")
	user := &mochi.Client{}
	setIdentity(user, kindUser, testAlice, scopeAll)
	inline := &mochi.Client{}
	inline.Net.Inline = true

	publish := func(cl *mochi.Client, suffix string, qos byte, retain bool, payload string) error {
		pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: qos, Retain: retain},
			TopicName: Topic(deviceID, suffix), Payload: []byte(payload)}
		_, err := h.OnPublish(cl, pk)
		return err
	}

	for name, cl := range map[string]*mochi.Client{"device": device, "user": user, "claim-wait": claimWaitClient(deviceID), "anonymous": {}} {
		if err := publish(cl, SuffixLogSession, 1, false, `{"action":"start"}`); !errors.Is(err, packets.ErrRejectPacket) {
			t.Errorf("%s published a log session message", name)
		}
	}
	if err := publish(inline, SuffixLogSession, 1, false, `{}`); err != nil {
		t.Errorf("the Hub's log session message refused: %v", err)
	}
	if err := publish(inline, SuffixLogSession, 1, true, `{}`); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("the Hub's log session message was retained")
	}

	batch := `{"session":"` + primitive.NewObjectID().Hex() + `","seq":0,"lines":[]}`
	if err := publish(device, SuffixLogStream, 1, true, batch); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("a retained batch was accepted")
	}
	if err := publish(device, SuffixLogStream, 2, false, batch); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("a QoS 2 batch was accepted")
	}

	// A malformed batch is dropped, not refused: refusing a QoS 1 publish
	// closes an MQTT 3.1.1 connection.
	if err := publish(device, SuffixLogStream, 1, false, `not json`); !errors.Is(err, packets.CodeSuccessIgnore) {
		t.Errorf("a malformed batch was not dropped (acknowledged, never delivered): %v", err)
	}
	if len(h.jobs) != 0 {
		t.Errorf("%d writes queued for a live log batch", len(h.jobs))
	}
}

func insertLogSession(t *testing.T, client *mongo.Client, deviceID primitive.ObjectID, mutate func(*devices.LogSession)) devices.LogSession {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	session := devices.LogSession{
		ID: primitive.NewObjectID(), DeviceID: deviceID, Owner: testAlice, CreatedBy: testAlice,
		Rev: "current", Sources: []string{"pantavisor/pantavisor.log"}, Tail: 10, Follow: true,
		CreatedAt: now, ExpiresAt: now.Add(devices.LogSessionLease), Deadline: now.Add(devices.LogSessionMaxDuration),
		Live: true, DeviceSlot: 0, UserSlot: 0,
	}
	if mutate != nil {
		mutate(&session)
	}
	if _, err := client.Database(utils.MongoDb).Collection(devices.LogSessionsCollection).InsertOne(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func loadLogSession(t *testing.T, client *mongo.Client, id primitive.ObjectID) devices.LogSession {
	t.Helper()
	session := devices.LogSession{}
	err := client.Database(utils.MongoDb).Collection(devices.LogSessionsCollection).
		FindOne(context.Background(), bson.M{"_id": id}).Decode(&session)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func countBatches(t *testing.T, client *mongo.Client, session primitive.ObjectID) int64 {
	t.Helper()
	n, err := client.Database(utils.MongoDb).Collection(devices.LogStreamCollection).
		CountDocuments(context.Background(), bson.M{"session": session})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func batchPayload(session primitive.ObjectID, seq int, end bool, reason string) string {
	raw, _ := json.Marshal(map[string]interface{}{
		"session": session.Hex(), "seq": seq, "end": end, "reason": reason, "dropped": 0,
		"lines": []map[string]string{{"src": "pantavisor/pantavisor.log", "line": "line " + strconv.Itoa(seq)}},
	})
	return string(raw)
}

// The bridge stores a batch only for a live session of the publishing device,
// ends the session on its last batch, and never touches the persisted logs.
func TestIngestLogStream(t *testing.T) {
	client := newTestMongo(t)
	h := newTestBridge()
	h.mongoClient = client

	device := primitive.NewObjectID()
	other := primitive.NewObjectID()
	cl := &mochi.Client{}
	setIdentity(cl, kindDevice, device.Hex(), "")

	publish := func(topicDevice primitive.ObjectID, payload string) error {
		pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: 0},
			TopicName: Topic(topicDevice.Hex(), SuffixLogStream), Payload: []byte(payload)}
		_, err := h.OnPublish(cl, pk)
		switch {
		case err == nil:
			// A batch the broker would fan out to subscribers.
			return errors.New("batch not ignored by the broker")
		case errors.Is(err, packets.CodeSuccessIgnore):
			return nil
		}
		return err
	}

	live := insertLogSession(t, client, device, nil)
	if err := publish(device, batchPayload(live.ID, 0, false, "")); err != nil {
		t.Fatal(err)
	}
	if n := countBatches(t, client, live.ID); n != 1 {
		t.Fatalf("%d batches stored right after the publish, want 1", n)
	}

	othersSession := insertLogSession(t, client, other, nil)
	ended := insertLogSession(t, client, device, func(s *devices.LogSession) {
		endedAt := time.Now().UTC().Add(-time.Minute)
		s.Live, s.EndedAt, s.Reason, s.EndedBy = false, &endedAt, devices.LogEndStopped, devices.LogEndedByHub
	})
	lapsed := insertLogSession(t, client, device, func(s *devices.LogSession) {
		s.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	})
	unknown := primitive.NewObjectID()
	for name, session := range map[string]primitive.ObjectID{
		"another device's session": othersSession.ID, "an ended session": ended.ID,
		"a lapsed session": lapsed.ID, "an unknown session": unknown,
	} {
		if err := publish(device, batchPayload(session, 0, false, "")); err != nil {
			t.Errorf("a batch for %s was refused rather than dropped: %v", name, err)
		}
		if n := countBatches(t, client, session); n != 0 {
			t.Errorf("a batch for %s was stored", name)
		}
	}
	if loadLogSession(t, client, othersSession.ID).Live != true {
		t.Error("another device's session was touched")
	}

	// On another device's topic: the session does not own that namespace.
	if err := publish(other, batchPayload(othersSession.ID, 0, false, "")); !errors.Is(err, packets.ErrRejectPacket) {
		t.Error("a device streamed on another device's topic")
	}
	if n := countBatches(t, client, othersSession.ID); n != 0 {
		t.Error("a batch on another device's topic was stored")
	}

	// The last batch ends the session with the device's reason.
	if err := publish(device, batchPayload(live.ID, 1, true, "done")); err != nil {
		t.Fatal(err)
	}
	got := loadLogSession(t, client, live.ID)
	if got.Live || got.Reason != "done" || got.EndedBy != devices.LogEndedByDevice || got.EndedAt == nil {
		t.Errorf("after the last batch: %+v", got)
	}

	// Nothing went to the persisted logs: no write queued, and the bridge
	// has no logs app to reach.
	if len(h.jobs) != 0 {
		t.Errorf("%d writes queued by live log batches", len(h.jobs))
	}
	n, err := client.Database(utils.MongoDb).Collection("pantahub_logs").CountDocuments(context.Background(), bson.M{})
	if err != nil || n != 0 {
		t.Errorf("%d persisted log entries (%v)", n, err)
	}

	// While the persisted logs topic still queues its write, as before.
	pk := packets.Packet{FixedHeader: packets.FixedHeader{Type: packets.Publish, Qos: 1},
		TopicName: Topic(device.Hex(), SuffixLogs), Payload: []byte(`[{"msg":"persisted"}]`)}
	if _, err := h.OnPublish(cl, pk); err != nil {
		t.Fatal(err)
	}
	if len(h.jobs) != 1 {
		t.Errorf("%d writes queued for the persisted logs topic, want 1", len(h.jobs))
	}
}

func TestLogSessionMessage(t *testing.T) {
	now := time.Date(2026, 9, 28, 15, 9, 0, 0, time.UTC)
	session := devices.LogSession{
		ID: primitive.NewObjectID(), DeviceID: primitive.NewObjectID(),
		Rev: "current", Sources: []string{"pantavisor/pantavisor.log", "telegraf/lxc/console.log"},
		Tail: 200, Follow: true, Live: true,
		ExpiresAt: now.Add(time.Minute), Deadline: now.Add(30 * time.Minute),
	}

	topic, payload, ok := logSessionMessage(&session, logActionStart, now)
	if !ok {
		t.Fatal("start not sent")
	}
	if topic != "ph/v1/dev/"+session.DeviceID.Hex()+"/logs/session" {
		t.Errorf("topic = %q", topic)
	}
	want := `{"id":"` + session.ID.Hex() + `","action":"start","rev":"current",` +
		`"sources":["pantavisor/pantavisor.log","telegraf/lxc/console.log"],"tail":200,"follow":true,` +
		`"expires_at":"2026-09-28T15:10:00Z","deadline":"2026-09-28T15:39:00Z"}`
	if string(payload) != want {
		t.Errorf("payload = %s\nwant      %s", payload, want)
	}

	if _, _, ok := logSessionMessage(&session, logActionStart, now.Add(time.Minute)); ok {
		t.Error("a start past the lease was sent")
	}
	if _, _, ok := logSessionMessage(&session, logActionRenew, now.Add(time.Hour)); ok {
		t.Error("a renew past the deadline was sent")
	}
	if _, _, ok := logSessionMessage(&session, logActionStop, now.Add(time.Hour)); !ok {
		t.Error("a stop past the lease was not sent")
	}
	noDevice := session
	noDevice.DeviceID = primitive.NilObjectID
	if _, _, ok := logSessionMessage(&noDevice, logActionStart, now); ok {
		t.Error("a session without a device was sent")
	}
}

func TestLogSessionAction(t *testing.T) {
	updated := func(fields bson.M) *changeEvent {
		raw, _ := bson.Marshal(fields)
		event := &changeEvent{OperationType: "update"}
		event.UpdateDescription.UpdatedFields = raw
		return event
	}
	live := &devices.LogSession{Live: true}
	stoppedByHub := &devices.LogSession{Live: false, EndedBy: devices.LogEndedByHub}
	endedByDevice := &devices.LogSession{Live: false, EndedBy: devices.LogEndedByDevice}

	for _, tc := range []struct {
		name    string
		event   *changeEvent
		session *devices.LogSession
		want    string
	}{
		{"insert", &changeEvent{OperationType: "insert"}, live, logActionStart},
		{"renew", updated(bson.M{"expires_at": time.Now()}), live, logActionRenew},
		{"stop", updated(bson.M{"live": false, "reason": "stopped"}), stoppedByHub, logActionStop},
		{"renew of a session stopped since", updated(bson.M{"expires_at": time.Now()}), stoppedByHub, ""},
		{"ended by the device", updated(bson.M{"live": false, "reason": "done"}), endedByDevice, ""},
		{"other update", updated(bson.M{"reason": "x"}), live, ""},
		{"delete", &changeEvent{OperationType: "delete"}, live, ""},
	} {
		action, ok := logSessionAction(tc.event, tc.session)
		if (tc.want == "") == ok || (ok && action != tc.want) {
			t.Errorf("%s: action %q, ok %v, want %q", tc.name, action, ok, tc.want)
		}
	}
}

// waitLogSessionStreams returns once the log sessions change stream of every
// broker delivers: a probe device's session shows up on each.
func waitLogSessionStreams(t *testing.T, client *mongo.Client, brokers ...*claimBroker) {
	t.Helper()
	probe := primitive.NewObjectID()
	seen := make([]chan struct{}, len(brokers))
	for i, b := range brokers {
		ch := make(chan struct{}, 64)
		seen[i] = ch
		err := b.server.Subscribe(Topic(probe.Hex(), SuffixLogSession), 90+i, func(_ *mochi.Client, _ packets.Subscription, _ packets.Packet) {
			select {
			case ch <- struct{}{}:
			default:
			}
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for i := range brokers {
		for delivered := false; !delivered; {
			insertLogSession(t, client, probe, nil)
			select {
			case <-seen[i]:
				delivered = true
			case <-time.After(300 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				t.Fatal("the log sessions change stream never opened")
			}
		}
	}
}

// nextPublish returns the next PUBLISH the client receives within d.
func (c *wireClient) nextPublish(t *testing.T, d time.Duration) packets.Packet {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		pk, ok := c.next(time.Until(deadline))
		if !ok {
			break
		}
		if pk.FixedHeader.Type == packets.Publish {
			return pk
		}
	}
	t.Fatal("no PUBLISH received")
	return packets.Packet{}
}

// Two replicas: the device holds its connection on B. Opening, renewing and
// stopping a session reaches it there, wherever the REST call was served,
// and nothing reaches a device on A that the session is not for.
func TestNotifierDeliversLogSessionsAcrossReplicas(t *testing.T) {
	client := newTestMongo(t)
	a := newClaimBroker(t, client, "A", 0)
	b := newClaimBroker(t, client, "B", 0)

	device := insertDevice(t, client, testAlice, nil)
	bystander := insertDevice(t, client, testAlice, nil)

	onB, code := dialWire(t, b.server, 5, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("device CONNECT refused: %x", code)
	}
	if codes := onB.subscribe(t, Topic(device.Hex(), SuffixLogSession)); len(codes) != 1 || !granted(codes[0]) {
		t.Fatalf("device refused its logs/session: %v", codes)
	}
	if codes := onB.subscribe(t, Topic(bystander.Hex(), SuffixLogSession)); len(codes) != 1 || granted(codes[0]) {
		t.Fatalf("device granted another device's logs/session: %v", codes)
	}

	onA, code := dialWire(t, a.server, 5, connectParams(bystander))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("bystander CONNECT refused: %x", code)
	}
	if codes := onA.subscribe(t, Topic(bystander.Hex(), SuffixLogSession)); len(codes) != 1 || !granted(codes[0]) {
		t.Fatalf("bystander refused its logs/session: %v", codes)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.notifier.Run(ctx)
	go b.notifier.Run(ctx)
	waitLogSessionStreams(t, client, a, b)

	expect := func(action string, session devices.LogSession) logSessionNotice {
		t.Helper()
		pk := onB.nextPublish(t, 10*time.Second)
		if pk.TopicName != Topic(device.Hex(), SuffixLogSession) {
			t.Fatalf("published on %q", pk.TopicName)
		}
		if pk.FixedHeader.Retain {
			t.Error("log session message retained")
		}
		if pk.FixedHeader.Qos != 1 {
			t.Errorf("log session message at QoS %d", pk.FixedHeader.Qos)
		}
		notice := logSessionNotice{}
		if err := json.Unmarshal(pk.Payload, &notice); err != nil {
			t.Fatalf("payload %s: %v", pk.Payload, err)
		}
		// The message expires with the lease it carries.
		expiresAt, err := time.Parse(time.RFC3339, notice.ExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		if expiry := pk.Properties.MessageExpiryInterval; expiry == 0 || time.Duration(expiry)*time.Second > time.Until(expiresAt)+time.Second {
			t.Errorf("message expiry %d s, lease ends in %v", expiry, time.Until(expiresAt))
		}
		if notice.Action != action || notice.ID != session.ID.Hex() {
			t.Fatalf("got %s, want %s of %s", pk.Payload, action, session.ID.Hex())
		}
		return notice
	}

	sessions := client.Database(utils.MongoDb).Collection(devices.LogSessionsCollection)
	session := insertLogSession(t, client, device, nil)
	notice := expect(logActionStart, session)
	if notice.Rev != "current" || len(notice.Sources) != 1 || notice.Tail != 10 || !notice.Follow ||
		notice.ExpiresAt != session.ExpiresAt.Format(time.RFC3339) || notice.Deadline != session.Deadline.Format(time.RFC3339) {
		t.Errorf("start = %+v", notice)
	}

	renewed := session.ExpiresAt.Add(20 * time.Second)
	if _, err := sessions.UpdateOne(ctx, bson.M{"_id": session.ID}, bson.M{"$set": bson.M{"expires_at": renewed}}); err != nil {
		t.Fatal(err)
	}
	if notice := expect(logActionRenew, session); notice.ExpiresAt != renewed.Format(time.RFC3339) {
		t.Errorf("renew expires_at = %s, want %s", notice.ExpiresAt, renewed.Format(time.RFC3339))
	}

	if _, err := sessions.UpdateOne(ctx, bson.M{"_id": session.ID}, bson.M{"$set": bson.M{
		"live": false, "ended_at": time.Now().UTC(), "ended_by": devices.LogEndedByHub, "reason": devices.LogEndStopped,
	}}); err != nil {
		t.Fatal(err)
	}
	expect(logActionStop, session)

	// A session the device ended itself is not stopped again.
	own := insertLogSession(t, client, device, func(s *devices.LogSession) { s.DeviceSlot = 1 })
	expect(logActionStart, own)
	if _, err := sessions.UpdateOne(ctx, bson.M{"_id": own.ID}, bson.M{"$set": bson.M{
		"live": false, "ended_at": time.Now().UTC(), "ended_by": devices.LogEndedByDevice, "reason": "done",
	}}); err != nil {
		t.Fatal(err)
	}
	onB.expectSilence(t, time.Second)

	// The bystander on A heard none of it.
	onA.expectSilence(t, 100*time.Millisecond)
}

func TestLogDropsAreReported(t *testing.T) {
	d := &dropCounter{window: time.Minute, max: 2}
	now := time.Now()
	errDrop := errors.New("no live session")
	expect(t, d.note("dev1", errDrop, now), "the first drop is reported at once")
	expect(t, !d.note("dev1", errDrop, now.Add(time.Second)), "then quiet within the window")
	expect(t, !d.note("dev1", errDrop, now.Add(30*time.Second)), "still quiet")
	expect(t, d.note("dev1", errDrop, now.Add(61*time.Second)), "a summary once the window passed")
	expect(t, d.note("dev1", errDrop, now.Add(62*time.Second)), "and a new window starts")

	// Bounded: quiet windows are forgotten to make room.
	expect(t, d.note("dev2", errDrop, now.Add(62*time.Second)), "dev2 first drop")
	expect(t, d.note("dev3", errDrop, now.Add(200*time.Second)), "dev3 first drop")
	if len(d.seen) > 2 {
		t.Fatalf("%d windows kept", len(d.seen))
	}
}

func expect(t *testing.T, ok bool, msg string) {
	t.Helper()
	if !ok {
		t.Error(msg)
	}
}

// A user's wildcard subscription over its own device (granted: it also covers
// status, device-meta and the rest a user may read) never delivers the live
// log topics. logs/stream is not fanned out to anyone (the bridge ignores it
// once stored), and logs/session, which the device must keep receiving, is
// withheld from users by the ACL the broker runs again on every delivery with
// the concrete topic.
func TestUserWildcardsNeverDeliverLiveLogs(t *testing.T) {
	client := newTestMongo(t)
	b := newClaimBroker(t, client, "A", 0)

	device := insertDevice(t, client, testAlice, nil)
	own := device.Hex()
	session := insertLogSession(t, client, device, nil)

	dev, code := dialWire(t, b.server, 5, connectParams(device))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("device CONNECT refused: %#x", code)
	}
	if codes := dev.subscribe(t, Topic(own, SuffixLogSession)); !granted(codes[0]) {
		t.Fatalf("device refused its logs/session: %v", codes)
	}

	filters := []string{
		Prefix + own + "/#", Prefix + own + "/logs/#", Prefix + own + "/logs/+",
		Prefix + own + "/+/stream", Prefix + own + "/+/session", Prefix + own + "/+",
	}
	for _, version := range []byte{4, 5} {
		user, code := dialWire(t, b.server, version, packets.ConnectParams{
			Clean: true, Keepalive: 60, ClientIdentifier: "dashboard-alice-" + strconv.Itoa(int(version)),
			Username: []byte(devicePrnPrefix + own),
			Password: []byte(signToken(t, jwtgo.MapClaims{"type": "USER", "prn": testAlice, "scopes": scopeReadOnly})),
		})
		if code != packets.CodeSuccess.Code {
			t.Fatalf("user CONNECT refused: %#x", code)
		}
		codes := user.subscribe(t, filters...)
		if len(codes) != len(filters) || !granted(codes[0]) {
			t.Fatalf("v%d: the owner lost its device's %s: %v", version, filters[0], codes)
		}

		// A batch the bridge stores, and one it drops (unknown session).
		seq := int(version) * 10
		dev.publish(t, Topic(own, SuffixLogStream), batchPayload(session.ID, seq, false, ""), 1, false)
		// Ignored by the broker, but still acknowledged to the device.
		if ack, ok := dev.next(5 * time.Second); !ok || ack.FixedHeader.Type != packets.Puback {
			t.Fatalf("v%d: no PUBACK for a stored batch: %+v", version, ack.FixedHeader)
		}
		dev.publish(t, Topic(own, SuffixLogStream), batchPayload(primitive.NewObjectID(), 0, false, ""), 0, false)
		// A session notice from the Hub reaches the device alone.
		if err := b.server.Publish(Topic(own, SuffixLogSession), []byte(`{"id":"`+session.ID.Hex()+`","action":"renew"}`), false, 1); err != nil {
			t.Fatal(err)
		}
		// Nor does a claimed message (the Hub sends it to claim-wait sessions).
		if err := b.server.Publish(Topic(own, SuffixClaimed), []byte(`{"owner":"`+testAlice+`"}`), false, 1); err != nil {
			t.Fatal(err)
		}
		if pk := dev.nextPublish(t, 5*time.Second); pk.TopicName != Topic(own, SuffixLogSession) {
			t.Fatalf("device got %q, want its logs/session", pk.TopicName)
		}
		// What a user may read still arrives, after all of the above.
		dev.publish(t, Topic(own, SuffixStatus), `{"status":"online"}`, 0, false)

		pk := user.nextPublish(t, 5*time.Second)
		if pk.TopicName != Topic(own, SuffixStatus) {
			t.Fatalf("v%d: user received %q (%s)", version, pk.TopicName, pk.Payload)
		}
		// Only one copy even though /# is not the only matching filter:
		// the wildcards other than /# match nothing a user may read.
		user.expectSilence(t, 200*time.Millisecond)
	}

	// The batch was stored before it was ignored.
	if n := countBatches(t, client, session.ID); n != 2 {
		t.Errorf("%d batches stored, want 2", n)
	}
}

// The log sessions change stream carries only what logSessionAction acts on:
// a stored batch's budget update of its session wakes no replica.
func TestLogSessionPipelineSkipsBudgetUpdates(t *testing.T) {
	client := newTestMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sessions := client.Database(utils.MongoDb).Collection(devices.LogSessionsCollection)
	// The collection must exist before a stream can be opened on it.
	session := insertLogSession(t, client, primitive.NewObjectID(), nil)

	stream, err := sessions.Watch(ctx, logSessionPipeline(), options.ChangeStream().SetFullDocument(options.UpdateLookup))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close(context.Background())

	next := func() changeEvent {
		t.Helper()
		if !stream.Next(ctx) {
			t.Fatalf("no change event: %v", stream.Err())
		}
		event := changeEvent{}
		if err := stream.Decode(&event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	update := func(set bson.M) {
		t.Helper()
		if _, err := sessions.UpdateOne(ctx, bson.M{"_id": session.ID}, set); err != nil {
			t.Fatal(err)
		}
	}

	// Oplog order: the budget updates come first, so the first event seen
	// being the renew proves they were filtered out on the server.
	update(bson.M{"$inc": bson.M{"stream_batches": 1, "stream_bytes": 100}})
	update(bson.M{"$inc": bson.M{"stream_batches": 1, "stream_bytes": 100}})
	update(bson.M{"$set": bson.M{"reason": "unrelated"}})
	update(bson.M{"$set": bson.M{"expires_at": session.ExpiresAt.Add(20 * time.Second)}})
	if event := next(); event.OperationType != "update" || !updatedField(&event, "expires_at") {
		t.Fatalf("first event: %s %s, want the renew", event.OperationType, event.UpdateDescription.UpdatedFields)
	}

	update(bson.M{"$inc": bson.M{"stream_batches": 1, "stream_bytes": 100}})
	update(bson.M{"$set": bson.M{"live": false, "ended_by": devices.LogEndedByHub}})
	if event := next(); event.OperationType != "update" || !updatedField(&event, "live") {
		t.Fatalf("second event: %s %s, want the stop", event.OperationType, event.UpdateDescription.UpdatedFields)
	}

	insertLogSession(t, client, primitive.NewObjectID(), nil)
	if event := next(); event.OperationType != "insert" {
		t.Fatalf("third event: %s, want the start", event.OperationType)
	}
}
