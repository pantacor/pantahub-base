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

package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// bridgeWorkers bounds how many device writes may be in flight at once.
	// Publishing runs on the broker's read loop, so the write must be
	// asynchronous — but one goroutine per message would let a chatty fleet
	// open an unbounded number of mongo sockets during a database stall. A
	// fixed pool gives the asynchrony without the fan-out.
	bridgeWorkers = 8

	// bridgeQueueDepth absorbs bursts (a fleet reconnecting after a network
	// partition). Past it, jobs are dropped with a log line: back-pressuring
	// the broker's read loop is worse than losing a metadata update, which
	// the device will resend on its next report.
	bridgeQueueDepth = 1024

	// bridgeJobTimeout caps the total lifetime of one ingest job, including
	// the device lookup it may need. No mongo call ever runs on an unbounded
	// context.
	bridgeJobTimeout = 30 * time.Second

	// bridgeWriteTimeout is the per-collection call budget, matching the
	// 10 seconds the REST handlers use.
	bridgeWriteTimeout = 10 * time.Second

	// maxStatusLength caps what a device can write into its liveness record.
	// The status suffix fires on every connect and disconnect and must not
	// become a side channel for storing payloads in device-meta.
	maxStatusLength = 64

	// statusOffline is the only status word that means "not alive"; the
	// device's last will carries it.
	statusOffline = "offline"
)

// bridgeJob is one decoded device report waiting to be written. The payload is
// already unmarshalled: workers never touch the packet, so nothing broker-owned
// escapes the read loop and no state is shared between the two.
type bridgeJob struct {
	topic string
	write func(ctx context.Context) error
}

// bridgeHook ingests what devices publish into the same MongoDB state the REST
// API writes, so that a device on a warm MQTT connection is indistinguishable
// from one that polls /trails, /devices/{id}/device-meta and /logs.
type bridgeHook struct {
	mochi.HookBase
	mongoClient *mongo.Client

	devices  *devices.App
	logs     *logs.App
	jobs     chan bridgeJob
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// ID identifies the hook in broker logs.
func (h *bridgeHook) ID() string {
	return "pantahub-bridge"
}

// Provides declares the hook points this bridge implements.
func (h *bridgeHook) Provides(b byte) bool {
	switch b {
	case mochi.OnPublish, mochi.OnWillSent:
		return true
	}
	return false
}

// Init starts the writer pool. mochi calls it exactly once, from AddHook,
// before any listener is serving.
func (h *bridgeHook) Init(config any) error {
	if h.mongoClient == nil {
		return errors.New("mqtt: bridge: no mongo client")
	}

	h.devices = devices.Build(h.mongoClient, nil)
	h.jobs = make(chan bridgeJob, bridgeQueueDepth)
	h.done = make(chan struct{})

	if h.logs == nil {
		return errors.New("mqtt: bridge: no logs app")
	}

	for i := 0; i < bridgeWorkers; i++ {
		h.wg.Add(1)
		go h.worker()
	}

	return nil
}

// Stop shuts the writer pool down once the in-flight writes have finished.
// mochi calls it when the broker closes. Reports still queued at that point are
// abandoned rather than held onto: the device resends on its next report, and
// blocking shutdown on a database that may be the reason for it is worse.
func (h *bridgeHook) Stop() error {
	h.stopOnce.Do(func() {
		if h.done != nil {
			close(h.done)
		}
	})
	h.wg.Wait()
	return nil
}

// OnPublish validates a device report and queues it for persistence.
//
// This is OnPublish rather than OnPublished because OnPublished returns
// nothing: it cannot refuse a packet, and a malformed payload has to be
// refused rather than delivered to whoever subscribed. OnPublish is the only
// hook point mochi lets a hook reject from — returning packets.ErrRejectPacket
// makes the server drop this one packet (server.go processPublish) while
// leaving the connection, and every other client, untouched.
//
// The "persist only what the broker accepted" property is kept by doing the
// work in two halves: the payload is decoded and authorized synchronously, so
// a rejected packet is never queued, and the mongo write is handed to the
// worker pool, which runs after this call returns and therefore after the
// broker has taken the packet.
func (h *bridgeHook) OnPublish(cl *mochi.Client, pk packets.Packet) (packets.Packet, error) {
	if err := h.ingest(cl, pk.TopicName, pk.Payload); err != nil {
		log.Printf("mqtt: bridge: rejecting publish on %s: %v", pk.TopicName, err)
		return pk, packets.ErrRejectPacket
	}

	return pk, nil
}

// OnWillSent ingests a device's last will. The broker builds the will packet
// itself and delivers it without passing it through OnPublish (server.sendLWT),
// so this is the only way the liveness record is written on an ungraceful
// disconnect — which is precisely when it matters. There is nothing left to
// reject here: the packet has already gone out to subscribers, so a malformed
// will is logged and dropped.
func (h *bridgeHook) OnWillSent(cl *mochi.Client, pk packets.Packet) {
	if err := h.ingest(cl, pk.TopicName, pk.Payload); err != nil {
		log.Printf("mqtt: bridge: dropping will on %s: %v", pk.TopicName, err)
	}
}

// ingest validates one device report and queues the write. It returns an error
// only for reports that are addressed at the Hub and unusable; a topic the
// bridge does not persist is not an error.
//
// The payload is decoded here, on the caller's goroutine, so that a bad payload
// can still be refused and so that the worker only ever sees decoded values.
func (h *bridgeHook) ingest(cl *mochi.Client, topic string, payload []byte) error {
	// Hub-originated publishes (retained steps/new, user-meta) travel through
	// the inline client and are not device reports.
	if cl == nil || cl.Net.Inline {
		return nil
	}

	deviceID, suffix, ok := Parse(topic)
	if !ok || !DeviceMayPublish(suffix) {
		return nil
	}

	// The device id comes from the topic, never from the payload, and is
	// cross-checked against the authenticated session below.
	if !sessionOwnsDevice(cl, deviceID) {
		return fmt.Errorf("session may not report for device %s", deviceID)
	}

	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		return fmt.Errorf("invalid device id %s: %w", deviceID, err)
	}

	switch suffix {
	case SuffixDeviceMeta:
		if len(payload) == 0 {
			return errors.New("empty device-meta")
		}
		data := map[string]interface{}{}
		if err := json.Unmarshal(payload, &data); err != nil {
			return fmt.Errorf("malformed device-meta: %w", err)
		}
		h.enqueue(topic, func(ctx context.Context) error {
			return h.patchDeviceMeta(ctx, deviceObjectID, data)
		})

	case SuffixStatus:
		// An empty payload is the MQTT idiom for clearing a retained
		// message: let the broker have it, record nothing.
		if len(payload) == 0 {
			return nil
		}
		data := statusMeta(payload)
		h.enqueue(topic, func(ctx context.Context) error {
			return h.patchDeviceMeta(ctx, deviceObjectID, data)
		})

	case SuffixLogs:
		entries, err := unmarshalLogEntries(payload)
		if err != nil {
			return fmt.Errorf("malformed logs: %w", err)
		}
		if len(entries) == 0 {
			return nil
		}
		h.enqueue(topic, func(ctx context.Context) error {
			return h.postLogs(ctx, deviceObjectID, entries)
		})

	default:
		rev, ok := ParseProgress(suffix)
		if !ok {
			return nil
		}
		progress := trailmodels.StepProgress{}
		if err := json.Unmarshal(payload, &progress); err != nil {
			return fmt.Errorf("malformed progress for rev %d: %w", rev, err)
		}
		// The REST handler stores whatever status the device reports; the
		// only requirement is that there is one. No state machine here
		// either — inventing one would diverge from /trails.
		if strings.TrimSpace(progress.Status) == "" {
			return fmt.Errorf("progress for rev %d has no status", rev)
		}
		h.enqueue(topic, func(ctx context.Context) error {
			return h.putStepProgress(ctx, deviceObjectID, rev, progress)
		})
	}

	return nil
}

// sessionOwnsDevice reports whether the session that published owns the device
// namespace the report is written into.
//
// The subject comes from the identity the auth hook recorded at CONNECT, which
// is unforgeable per connection, and the device id comes from the topic. Only a
// device identity reports about itself: a user session may publish commands to
// a device it owns, but nothing a user writes is device state. The ACL hook
// already denies both cases; this is the redundant check that keeps an ACL
// regression from becoming a cross-device write.
func sessionOwnsDevice(cl *mochi.Client, deviceID string) bool {
	kind, subject := identity(cl)
	return kind == kindDevice && subject == deviceID
}

// enqueue hands a decoded report to the writer pool. It never blocks: the
// caller is the broker's read loop for this client.
func (h *bridgeHook) enqueue(topic string, write func(ctx context.Context) error) {
	select {
	case <-h.done:
		return
	default:
	}

	select {
	case h.jobs <- bridgeJob{topic: topic, write: write}:
	default:
		log.Printf("mqtt: bridge: write queue full, dropping %s", topic)
	}
}

func (h *bridgeHook) worker() {
	defer h.wg.Done()

	for {
		select {
		case <-h.done:
			return
		case job := <-h.jobs:
			h.runJob(job)
		}
	}
}

// runJob executes one write under a bounded context. A failing or panicking
// write costs one report, never the worker and never the broker.
func (h *bridgeHook) runJob(job bridgeJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("mqtt: bridge: recovered while writing %s: %v", job.topic, r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), bridgeJobTimeout)
	defer cancel()

	if err := job.write(ctx); err != nil {
		log.Printf("mqtt: bridge: %s: %v", job.topic, err)
	}
}

// putStepProgress mirrors trails.handlePutStepProgress: a blind $set of the
// reported progress on the step document owned by the calling device, followed
// by a best-effort bump of the trail's last-touched.
func (h *bridgeHook) putStepProgress(parentCtx context.Context, deviceObjectID primitive.ObjectID, rev int, progress trailmodels.StepProgress) error {
	device := devices.Device{}
	if err := h.devices.FindDeviceByID(parentCtx, deviceObjectID, &device); err != nil {
		return fmt.Errorf("cannot read device: %w", err)
	}

	trailID := deviceObjectID.Hex()
	stepID := trailID + "-" + strconv.Itoa(rev)
	progressTime := time.Now()

	ctx, cancel := context.WithTimeout(parentCtx, bridgeWriteTimeout)
	defer cancel()

	steps := h.mongoClient.Database(utils.MongoDb).Collection(stepsCollection)
	updateResult, err := steps.UpdateOne(
		ctx,
		bson.M{
			"_id":     stepID,
			"device":  device.Prn,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{
			"progress":      progress,
			"progress-time": progressTime,
			"timemodified":  time.Now(),
			"ispublic":      device.IsPublic,
		}},
	)
	if err != nil {
		return fmt.Errorf("cannot update step progress: %w", err)
	}
	if updateResult.MatchedCount == 0 {
		return fmt.Errorf("cannot update step progress: step %s not found", stepID)
	}

	trailCtx, trailCancel := context.WithTimeout(parentCtx, bridgeWriteTimeout)
	defer trailCancel()

	trails := h.mongoClient.Database(utils.MongoDb).Collection(trailsCollection)
	_, err = trails.UpdateOne(
		trailCtx,
		bson.M{
			"_id":     deviceObjectID,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{"last-touched": progressTime}},
	)
	if err != nil {
		// Same call as the REST handler: the step was written, so this is
		// reported and not treated as a failure.
		return fmt.Errorf("step written but last-touched not updated for trail %s: %w", trailID, err)
	}

	return nil
}

// patchDeviceMeta mirrors devices.handlePatchDeviceData: keys are BSON-quoted,
// the map is flattened to dot notation so nested updates stay atomic, a null
// value unsets its key, and meta-modified is bumped. meta-modified is the
// liveness signal the rest of the Hub reads, so it is written on every patch,
// including the cheap status ones.
func (h *bridgeHook) patchDeviceMeta(parentCtx context.Context, deviceObjectID primitive.ObjectID, data map[string]interface{}) error {
	quoted := utils.BsonQuoteMap(&data)

	setFields := bson.M{}
	unsetFields := bson.M{}
	flattenDeviceMeta("device-meta", quoted, setFields, unsetFields)

	now := time.Now()
	setFields["timemodified"] = now
	setFields["meta-modified"] = now

	updateDoc := bson.M{"$set": setFields}
	if len(unsetFields) > 0 {
		updateDoc["$unset"] = unsetFields
	}

	ctx, cancel := context.WithTimeout(parentCtx, bridgeWriteTimeout)
	defer cancel()

	collection := h.mongoClient.Database(utils.MongoDb).Collection(devicesCollection)
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id":     deviceObjectID,
			"garbage": bson.M{"$ne": true},
		},
		updateDoc,
	)
	if err != nil {
		return fmt.Errorf("cannot update device-meta: %w", err)
	}
	if updateResult.MatchedCount == 0 {
		return fmt.Errorf("cannot update device-meta: device %s not found", deviceObjectID.Hex())
	}

	return nil
}

// postLogs enriches the reported entries with the identity of the session that
// published them and stores them, as logs.handlePostLogs does. Device and owner
// come from the device document, never from the payload.
func (h *bridgeHook) postLogs(parentCtx context.Context, deviceObjectID primitive.ObjectID, entries []logs.Entry) error {
	device := devices.Device{}
	if err := h.devices.FindDeviceByID(parentCtx, deviceObjectID, &device); err != nil {
		return fmt.Errorf("cannot read device: %w", err)
	}
	if device.Owner == "" {
		return fmt.Errorf("device %s has no owner", deviceObjectID.Hex())
	}

	now := time.Now()
	for i := range entries {
		entries[i].ID = primitive.NewObjectID()
		entries[i].Device = device.Prn
		entries[i].Owner = device.Owner
		entries[i].TimeCreated = now
		if entries[i].LogLevel == "" {
			entries[i].LogLevel = "INFO"
		}
	}

	ctx, cancel := context.WithTimeout(parentCtx, bridgeWriteTimeout)
	defer cancel()

	// Store through the same backend the REST API selected, so logs land in
	// elastic where elastic is configured and are therefore queryable through
	// GET /logs regardless of which transport carried them.
	if err := h.logs.PostLogs(ctx, entries); err != nil {
		return fmt.Errorf("cannot post logs: %w", err)
	}

	return nil
}

// unmarshalLogEntries accepts either a JSON array of entries or a single entry
// object, which is what the /logs endpoint accepts.
func unmarshalLogEntries(payload []byte) ([]logs.Entry, error) {
	entries := make([]logs.Entry, 1)

	err := json.Unmarshal(payload, &entries)
	if err != nil {
		err = json.Unmarshal(payload, &entries[0])
	}
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// statusMeta turns a status payload into the device-meta patch that records
// liveness. Devices publish either a bare word ("online", "offline" — the
// latter is what the last will carries) or a small JSON object with an
// "online" boolean. Anything else still counts as alive, because the packet
// arrived. The result goes through the same quoting and merge path as any
// other device-meta patch, so the keys read back as "pantahub.online" etc.
func statusMeta(payload []byte) map[string]interface{} {
	status := strings.TrimSpace(string(payload))
	var online *bool

	var text string
	var doc struct {
		Online *bool  `json:"online"`
		Status string `json:"status"`
	}
	switch {
	case json.Unmarshal(payload, &text) == nil:
		status = strings.TrimSpace(text)
	case json.Unmarshal(payload, &doc) == nil:
		if doc.Status != "" {
			status = strings.TrimSpace(doc.Status)
		}
		online = doc.Online
	}

	if len(status) > maxStatusLength {
		status = strings.ToValidUTF8(status[:maxStatusLength], "")
	}

	if online == nil {
		up := !strings.EqualFold(status, statusOffline)
		online = &up
	}

	return map[string]interface{}{
		"pantahub.online":      *online,
		"pantahub.status":      status,
		"pantahub.status-time": time.Now().Format(time.RFC3339),
	}
}

// flattenDeviceMeta rewrites a nested map into the dot-notation $set and $unset
// fields a partial update needs, exactly as devices.flattenMap does: a nil
// value unsets its path, everything else is set at its full path. Keys are
// expected to be BSON-quoted already, so the dots this adds are only ever path
// separators.
func flattenDeviceMeta(prefix string, m map[string]interface{}, setFields bson.M, unsetFields bson.M) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		if v == nil {
			unsetFields[key] = ""
			continue
		}

		if nestedMap, ok := v.(map[string]interface{}); ok {
			flattenDeviceMeta(key, nestedMap, setFields, unsetFields)
		} else if nestedMap, ok := v.(bson.M); ok {
			flattenDeviceMeta(key, nestedMap, setFields, unsetFields)
		} else {
			setFields[key] = v
		}
	}
}
