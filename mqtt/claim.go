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
	"log"
	"strconv"
	"sync"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Claim-wait sessions: an unclaimed device keeps one MQTT connection open to
// learn the moment it is claimed, instead of polling the REST API.
//
//   - The device connects with its own credentials (device secret or device
//     token), client id = device id, clean session, no will (auth.go). Its
//     identity is kindClaimWait for the whole connection.
//   - It may subscribe to ph/v1/dev/<own id>/claimed and to nothing else, and
//     may publish nothing (authHook.OnACLCheck).
//   - When the device is claimed (the owner of its document goes from empty to
//     a user, on whichever replica served the REST call), every replica's
//     notifier sees the change on the pantahub_devices change stream; the
//     replica holding the session publishes {"owner": "<prn>"} on the claimed
//     topic, QoS 1, not retained, waits for the PUBACK and closes the session.
//     The device reconnects and, now claimed, gets a device session with the
//     full device ACL. A claim-wait session is never widened in place.
//   - Missed events are covered twice: the session's own subscribe re-reads
//     the device (a claim between CONNECT and SUBSCRIBE), and a periodic
//     sweep re-reads every claim-wait session's device (a change stream that
//     was down or lost its resume point).

const (
	// EnvMqttMaxClaimWaitSessions caps the claim-wait sessions one replica
	// holds at once. Registration is open, so anyone can mint unclaimed
	// devices and park a connection for each; past the cap a claim-wait
	// CONNECT is refused and the device falls back to REST polling. 0 turns
	// claim-wait sessions off altogether.
	EnvMqttMaxClaimWaitSessions = "PANTAHUB_MQTT_MAX_CLAIM_WAIT_SESSIONS"

	defaultMaxClaimWaitSessions = 5000

	// claimAckTimeout bounds how long the broker waits for the device to
	// acknowledge its claimed message before closing the session anyway.
	claimAckTimeout = 10 * time.Second
	claimAckPoll    = 20 * time.Millisecond

	// claimSweepInterval is how often every claim-wait session's device is
	// re-read, so that a claim whose change event was missed still ends the
	// session.
	claimSweepInterval = 30 * time.Second

	// claimSweepBatch bounds the ids of one sweep query.
	claimSweepBatch = 500

	// claimLookupTimeout bounds one device lookup of the subscribe check or
	// of a sweep batch.
	claimLookupTimeout = 5 * time.Second

	// maxClaimWaitKeepalive is the longest keepalive, in seconds, a
	// claim-wait session may ask for (see authHook.admitClaimWait).
	maxClaimWaitKeepalive = 300
)

// claimNotice is the payload published on the claimed topic.
type claimNotice struct {
	Owner string `json:"owner"`
}

// maxClaimWaitSessions reads EnvMqttMaxClaimWaitSessions. A malformed or
// negative value keeps the default rather than disabling the cap.
func maxClaimWaitSessions() int {
	raw := utils.GetEnvDefault(EnvMqttMaxClaimWaitSessions, "")
	if raw == "" {
		return defaultMaxClaimWaitSessions
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultMaxClaimWaitSessions
	}
	return n
}

// claimHook tracks the claim-wait sessions of this replica and ends each one
// with its claimed message once the device is claimed.
type claimHook struct {
	mochi.HookBase
	mongoClient *mongo.Client
	server      *mochi.Server
	maxSessions int
	ackTimeout  time.Duration

	mu sync.Mutex
	// sessions are the established claim-wait sessions of this replica.
	sessions map[*mochi.Client]struct{}
	// settling are the sessions being told of their claim, so that the
	// change stream, the subscribe check and the sweep tell each only once.
	settling map[*mochi.Client]struct{}
	// checked are the sessions whose subscribe check already ran: a client
	// repeating its SUBSCRIBE does not get a database read for each one.
	checked map[*mochi.Client]struct{}
}

func newClaimHook(mongoClient *mongo.Client, server *mochi.Server, maxSessions int) *claimHook {
	return &claimHook{
		mongoClient: mongoClient,
		server:      server,
		maxSessions: maxSessions,
		ackTimeout:  claimAckTimeout,
		sessions:    map[*mochi.Client]struct{}{},
		settling:    map[*mochi.Client]struct{}{},
		checked:     map[*mochi.Client]struct{}{},
	}
}

// ID identifies the hook in broker logs.
func (h *claimHook) ID() string {
	return "pantahub-claim"
}

// Provides declares the hook points this hook implements.
func (h *claimHook) Provides(b byte) bool {
	switch b {
	case mochi.OnSessionEstablished, mochi.OnSubscribed, mochi.OnDisconnect:
		return true
	}
	return false
}

// hasRoom reports whether another claim-wait session may be admitted. The
// count is of established sessions, so a burst of concurrent CONNECTs can
// overshoot the cap by the handful still between authentication and CONNACK.
func (h *claimHook) hasRoom() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions) < h.maxSessions
}

// count is the number of established claim-wait sessions.
func (h *claimHook) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// OnSessionEstablished counts a claim-wait session. mochi runs OnDisconnect
// for every session that reached this point, which uncounts it.
func (h *claimHook) OnSessionEstablished(cl *mochi.Client, _ packets.Packet) {
	if kind, _ := identity(cl); kind != kindClaimWait {
		return
	}
	h.mu.Lock()
	h.sessions[cl] = struct{}{}
	h.mu.Unlock()
}

// OnDisconnect forgets a claim-wait session.
func (h *claimHook) OnDisconnect(cl *mochi.Client, _ error, _ bool) {
	h.mu.Lock()
	delete(h.sessions, cl)
	delete(h.settling, cl)
	delete(h.checked, cl)
	h.mu.Unlock()
}

// OnSubscribed re-reads the device once its claimed subscription is granted:
// a claim that landed between the CONNECT lookup and now produced its change
// event before there was a subscription to deliver it to. It runs on the
// client's read goroutine, which must stay free to read the PUBACK, so the
// work is done on a goroutine of its own, once per session: later claims
// reach the session through the change stream or the sweep.
func (h *claimHook) OnSubscribed(cl *mochi.Client, pk packets.Packet, reasonCodes []byte) {
	kind, deviceID := identity(cl)
	if kind != kindClaimWait {
		return
	}
	claimed := Topic(deviceID, SuffixClaimed)
	for i, sub := range pk.Filters {
		if sub.Filter != claimed || i >= len(reasonCodes) || reasonCodes[i] >= packets.ErrUnspecifiedError.Code {
			continue
		}
		h.mu.Lock()
		_, done := h.checked[cl]
		h.checked[cl] = struct{}{}
		h.mu.Unlock()
		if !done {
			go h.recheck(cl, deviceID)
		}
		return
	}
}

// recheck settles one claim-wait session from its device's current state.
func (h *claimHook) recheck(cl *mochi.Client, deviceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), claimLookupTimeout)
	defer cancel()

	states, err := h.deviceStates(ctx, []string{deviceID})
	if err != nil {
		// The sweep tries again; the session stays as narrow as it is.
		log.Printf("mqtt: claim: cannot check device %s: %v", deviceID, err)
		return
	}
	h.apply(cl, deviceID, states)
}

// claimed is told by the notifier that deviceID now belongs to owner. It is a
// no-op unless this replica holds the device's claim-wait session.
func (h *claimHook) claimed(deviceID, owner string) {
	if h == nil || h.server == nil || owner == "" {
		return
	}
	cl, ok := h.server.Clients.Get(deviceID)
	if !ok {
		return
	}
	if kind, subject := identity(cl); kind != kindClaimWait || subject != deviceID {
		return
	}
	// A session that has not subscribed yet is told by its subscribe check,
	// which will read the device as claimed; closing it now would lose the
	// message.
	if _, subscribed := cl.State.Subscriptions.Get(Topic(deviceID, SuffixClaimed)); !subscribed {
		return
	}
	go h.settle(cl, deviceID, owner)
}

// deviceState is what a claim-wait session is judged by.
type deviceState struct {
	ID      primitive.ObjectID `bson:"_id"`
	Owner   string             `bson:"owner"`
	Garbage bool               `bson:"garbage"`
	// OVMode is read through the type the CONNECT check reads, so both
	// judge ownership verification alike.
	OVMode *models.OVModeExtension `bson:"ovmode"`
}

// apply settles cl from its device's state: a device that is gone, deleted
// or under ownership verification loses its session; a claimed device is
// told and loses it.
func (h *claimHook) apply(cl *mochi.Client, deviceID string, states map[string]*deviceState) {
	state, ok := states[deviceID]
	switch {
	case !ok || state.Garbage || state.OVMode.NeedsVerification():
		h.close(cl)
	case state.Owner != "":
		h.settle(cl, deviceID, state.Owner)
	}
}

// settle tells a claim-wait session its device was claimed and then closes
// it. The message goes out live, QoS 1 and not retained, and the session is
// closed once the device acknowledged it (or after ackTimeout), because the
// session is clean: a message still in flight at the close is gone.
func (h *claimHook) settle(cl *mochi.Client, deviceID, owner string) {
	h.mu.Lock()
	if _, busy := h.settling[cl]; busy {
		h.mu.Unlock()
		return
	}
	h.settling[cl] = struct{}{}
	h.mu.Unlock()

	topic := Topic(deviceID, SuffixClaimed)
	if _, subscribed := cl.State.Subscriptions.Get(topic); subscribed && !cl.Closed() {
		payload, err := json.Marshal(claimNotice{Owner: owner})
		if err == nil {
			err = h.server.Publish(topic, payload, false, notifierQoS)
		}
		if err != nil {
			log.Printf("mqtt: claim: cannot tell device %s it was claimed: %v", deviceID, err)
		} else {
			h.awaitAck(cl)
		}
	}
	h.close(cl)
}

// awaitAck waits until nothing is in flight to cl, which, for a session that
// can only ever receive its claimed message, means it was acknowledged.
func (h *claimHook) awaitAck(cl *mochi.Client) {
	deadline := time.Now().Add(h.ackTimeout)
	for cl.State.Inflight.Len() > 0 && !cl.Closed() && time.Now().Before(deadline) {
		time.Sleep(claimAckPoll)
	}
}

// close ends a claim-wait session. An MQTT 5 client is told why; MQTT 3.1.1
// has no server DISCONNECT, so the connection is just closed.
func (h *claimHook) close(cl *mochi.Client) {
	if cl.Closed() {
		return
	}
	if cl.Properties.ProtocolVersion == 5 && h.server != nil {
		_ = h.server.DisconnectClient(cl, packets.ErrAdministrativeAction)
		return
	}
	cl.Stop(packets.ErrAdministrativeAction)
}

// runSweep re-reads the devices of every claim-wait session until ctx is
// done.
func (h *claimHook) runSweep(ctx context.Context) {
	ticker := time.NewTicker(claimSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sweep(ctx)
		}
	}
}

// sweep settles every claim-wait session whose device is no longer waiting
// for a claim.
func (h *claimHook) sweep(ctx context.Context) {
	h.mu.Lock()
	clients := make(map[string]*mochi.Client, len(h.sessions))
	for cl := range h.sessions {
		if _, busy := h.settling[cl]; busy {
			continue
		}
		if _, deviceID := identity(cl); deviceID != "" {
			clients[deviceID] = cl
		}
	}
	h.mu.Unlock()

	ids := make([]string, 0, len(clients))
	for id := range clients {
		ids = append(ids, id)
	}

	for start := 0; start < len(ids) && ctx.Err() == nil; start += claimSweepBatch {
		batch := ids[start:min(start+claimSweepBatch, len(ids))]

		queryCtx, cancel := context.WithTimeout(ctx, claimLookupTimeout)
		states, err := h.deviceStates(queryCtx, batch)
		cancel()
		if err != nil {
			log.Printf("mqtt: claim: sweep cannot read %d device(s): %v", len(batch), err)
			continue
		}
		for _, id := range batch {
			go h.apply(clients[id], id, states)
		}
	}
}

// deviceStates reads the claim state of the given devices, keyed by hex id.
// A device missing from the answer does not exist.
func (h *claimHook) deviceStates(ctx context.Context, ids []string) (map[string]*deviceState, error) {
	if h.mongoClient == nil {
		return nil, errNoMongoClient
	}
	objectIDs := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		if objectID, err := primitive.ObjectIDFromHex(id); err == nil {
			objectIDs = append(objectIDs, objectID)
		}
	}

	cursor, err := h.mongoClient.Database(utils.MongoDb).Collection(devicesCollection).Find(ctx,
		bson.M{"_id": bson.M{"$in": objectIDs}},
		options.Find().SetProjection(bson.M{"_id": 1, "owner": 1, "garbage": 1, "ovmode": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	states := make(map[string]*deviceState, len(ids))
	for cursor.Next(ctx) {
		state := &deviceState{}
		if err := cursor.Decode(state); err != nil {
			return nil, err
		}
		states[state.ID.Hex()] = state
	}
	return states, cursor.Err()
}

// claimedOwner returns the owner a device update just set, when it set one.
func claimedOwner(event *changeEvent) (string, bool) {
	value, err := event.UpdateDescription.UpdatedFields.LookupErr("owner")
	if err != nil {
		return "", false
	}
	owner, ok := value.StringValueOK()
	return owner, ok && owner != ""
}
