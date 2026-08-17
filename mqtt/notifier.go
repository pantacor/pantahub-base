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
	"log"
	"strings"
	"sync"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// Retained notifications are the device's source of truth while it is
	// offline, so they are published at QoS 1: at-least-once, and the
	// retained copy makes a duplicate harmless.
	notifierQoS = 1

	// Auxiliary lookups are bounded; the watch loop itself is not, since a
	// change stream is a long poll that must survive quiet periods.
	notifierQueryTimeout = 10 * time.Second

	// Closing a stream must not inherit an already cancelled context, or the
	// killCursors never reaches the server.
	notifierCloseTimeout = 5 * time.Second

	notifierBackoffInitial = time.Second
	notifierBackoffMax     = time.Minute

	// A stream that stayed open this long counts as healthy: the next failure
	// starts backing off from scratch instead of from the previous ceiling.
	notifierBackoffReset = 2 * time.Minute

	// A notification publish goes to the in-process broker, so a failure is
	// rare and usually momentary (the inline client briefly unavailable). The
	// consume loop advances its resume token past an event once handled and the
	// live stream never redelivers, so a dropped publish is a dropped
	// notification until the trail's next change. A few short retries turn a
	// transient failure back into a delivery rather than a silent gap.
	notifierPublishAttempts   = 3
	notifierPublishRetryDelay = 200 * time.Millisecond
)

// Mongo error codes that mean the deployment cannot serve change streams at
// all (standalone mongod, or a server too old to know the pipeline stage).
// These are terminal: retrying cannot make a standalone become a replica set.
var notifierUnsupportedCodes = []int{
	40573, // $changeStream is only supported on replica sets
	40324, // Unrecognized pipeline stage name: $changeStream
	59,    // no such command: aggregate/getMore
}

// Mongo error codes that mean the resume point is gone (oplog rolled over, or
// a token from a different deployment). Retrying with the same token spins
// forever, so these force a restart from the current time.
var notifierResumeLostCodes = []int{
	280,   // ChangeStreamFatalError
	286,   // ChangeStreamHistoryLost
	40585, // resume of change stream was not possible
	40615, // resume token is not a valid change stream token
}

// Notifier pushes Hub state changes to devices over MQTT so that a device does
// not have to poll the REST API to learn about them.
//
// It watches MongoDB change streams and publishes RETAINED messages: a device
// that reconnects after any amount of downtime is told the current state by the
// broker at subscribe time, without the Hub having to replay history.
type Notifier struct {
	mongoClient *mongo.Client
	server      *mochi.Server

	// unsupportedOnce keeps the "no change streams here" warning to a single
	// line per process, however many watchers hit it.
	unsupportedOnce sync.Once
}

// NewNotifier builds a notifier bound to a mongo client and the broker whose
// inline client is used to publish. It starts nothing; call Run.
func NewNotifier(mongoClient *mongo.Client, server *mochi.Server) *Notifier {
	return &Notifier{
		mongoClient: mongoClient,
		server:      server,
	}
}

// Run watches the collections that devices care about until ctx is cancelled.
//
// It returns nil, not an error, when the deployment cannot serve change streams:
// push is an optimisation over polling, and the API must keep serving without
// it. Errors that are worth reporting are logged as they happen; the returned
// error only reflects context cancellation.
func (n *Notifier) Run(ctx context.Context) error {
	watchers := []struct {
		collection string
		pipeline   mongo.Pipeline
		handle     func(context.Context, *changeEvent)
		// onReady rebuilds the retained topics this collection owns, once the
		// change stream is open. The broker's retained messages live only in
		// memory and a stream opened now starts at the current cluster time, so
		// anything that changed while the process was down produced no event
		// the stream will see and the retained copy was lost with the old
		// process. The sweep runs *after* the stream is capturing, so an event
		// that lands during the sweep is delivered by the stream rather than
		// falling into the gap between snapshot and subscribe.
		onReady func(context.Context)
	}{
		{
			collection: stepsCollection,
			pipeline:   notifierPipeline("insert", "update", "replace"),
			handle:     n.handleStepChange,
			onReady:    n.reconcilePendingSteps,
		},
		{
			collection: devicesCollection,
			pipeline:   notifierPipeline("update"),
			handle:     n.handleDeviceChange,
			onReady:    n.reconcileUserMeta,
		},
	}

	var wg sync.WaitGroup
	for _, w := range watchers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.watch(ctx, w.collection, w.pipeline, w.handle, w.onReady)
		}()
	}
	wg.Wait()

	return ctx.Err()
}

// reconcilePendingSteps publishes the oldest outstanding NEW step for every
// trail that has one, rebuilding the retained steps/new topics after a restart.
// A publish reaches both devices already subscribed (as a live message) and
// devices that subscribe later (as the retained message), so the pending
// revision is delivered either way. It is best effort: an error on a single
// trail is logged and the sweep continues, because the change stream that is
// already open by the time this runs will still catch that trail's next
// transition.
func (n *Notifier) reconcilePendingSteps(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, notifierQueryTimeout)
	defer cancel()

	trailIDs, err := n.mongoClient.
		Database(utils.MongoDb).
		Collection(stepsCollection).
		Distinct(queryCtx, "trail-id", bson.M{
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		})
	if err != nil {
		// A deployment without change streams also cannot reconcile; that is
		// expected and already reported when the watchers start.
		log.Println("mqtt: notifier could not list trails with pending steps: " + err.Error())
		return
	}

	published := 0
	for _, raw := range trailIDs {
		if ctx.Err() != nil {
			return
		}
		trailID, ok := raw.(primitive.ObjectID)
		if !ok {
			continue
		}
		n.publishOldestNewStep(ctx, trailID)
		published++
	}

	if published > 0 {
		log.Printf("mqtt: notifier reconciled %d pending step(s) at startup", published)
	}
}

// reconcileUserMeta republishes the retained user-meta for every device that
// has any, rebuilding those topics after a restart for the same reason
// reconcilePendingSteps rebuilds steps/new: the retained copy was lost with the
// old broker and the change stream only carries future edits. Only devices with
// a non-empty user-meta are touched, so an idle fleet costs nothing, and each
// publish carries the same unquoted map handleDeviceChange would send.
func (n *Notifier) reconcileUserMeta(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, notifierQueryTimeout)
	defer cancel()

	cursor, err := n.mongoClient.
		Database(utils.MongoDb).
		Collection(devicesCollection).
		Find(queryCtx, bson.M{
			"user-meta": bson.M{"$exists": true, "$nin": bson.A{nil, bson.M{}}},
			"garbage":   bson.M{"$ne": true},
		}, options.Find().SetProjection(bson.M{"_id": 1, "user-meta": 1}))
	if err != nil {
		log.Println("mqtt: notifier could not list devices with user-meta: " + err.Error())
		return
	}
	defer cursor.Close(queryCtx)

	published := 0
	for cursor.Next(queryCtx) {
		if ctx.Err() != nil {
			return
		}

		var doc struct {
			ID       primitive.ObjectID     `bson:"_id"`
			UserMeta map[string]interface{} `bson:"user-meta"`
		}
		if err := cursor.Decode(&doc); err != nil {
			log.Println("mqtt: notifier could not decode a device for user-meta reconcile: " + err.Error())
			continue
		}
		if len(doc.UserMeta) == 0 {
			continue
		}

		payload, err := json.Marshal(utils.BsonUnquoteMap(&doc.UserMeta))
		if err != nil {
			log.Println("mqtt: notifier could not encode user-meta of device " + doc.ID.Hex() + ": " + err.Error())
			continue
		}

		n.publish(Topic(doc.ID.Hex(), SuffixUserMeta), payload)
		published++
	}
	if err := cursor.Err(); err != nil {
		log.Println("mqtt: notifier user-meta reconcile cursor ended: " + err.Error())
	}

	if published > 0 {
		log.Printf("mqtt: notifier reconciled user-meta for %d device(s) at startup", published)
	}
}

// notifierPipeline keeps uninteresting operation types on the server side, so a
// busy collection does not push every delete and drop over the wire.
func notifierPipeline(types ...string) mongo.Pipeline {
	return mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{
			"operationType": bson.M{"$in": types},
		}}},
	}
}

// changeEvent is the part of a change stream document the notifier uses.
// Everything is kept as bson.Raw so that an unexpected document shape is a
// lookup miss rather than a decode failure or a panic.
type changeEvent struct {
	OperationType     string   `bson:"operationType"`
	DocumentKey       bson.Raw `bson:"documentKey"`
	FullDocument      bson.Raw `bson:"fullDocument"`
	UpdateDescription struct {
		UpdatedFields bson.Raw `bson:"updatedFields"`
		RemovedFields []string `bson:"removedFields"`
	} `bson:"updateDescription"`
}

// watch runs one collection's change stream for the lifetime of ctx, resuming
// from the last seen token across transient failures with capped exponential
// backoff. It returns only on ctx cancellation or on a terminal condition.
func (n *Notifier) watch(
	ctx context.Context,
	collection string,
	pipeline mongo.Pipeline,
	handle func(context.Context, *changeEvent),
	onReady func(context.Context),
) {
	var resumeToken bson.Raw
	backoff := notifierBackoffInitial
	readied := false

	for ctx.Err() == nil {
		openedAt := time.Now()

		stream, err := n.openStream(ctx, collection, pipeline, resumeToken)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if isChangeStreamUnsupported(err) {
				n.warnUnsupported(err)
				return
			}
			// Reopening without a token starts the stream at the current
			// cluster time, which is the only way forward once the resume
			// point has left the oplog. Only worth doing while we still hold
			// a token, otherwise the classification was wrong and dropping
			// straight back into the loop would spin.
			if isResumeLost(err) && len(resumeToken) > 0 {
				log.Println("mqtt: notifier resume point lost on " + collection + ", restarting from now: " + err.Error())
				resumeToken = nil
				continue
			}

			log.Println("mqtt: notifier cannot watch " + collection + ": " + err.Error())
			if !waitBackoff(ctx, backoff) {
				return
			}
			backoff = nextNotifierBackoff(backoff)
			continue
		}

		// The stream is now capturing from its start point, so the retained
		// topics can be rebuilt without a snapshot-to-subscribe gap: any change
		// during the sweep is already in this stream's history and will be
		// delivered by consume below. Only on the first open — reconnects must
		// not re-sweep the whole fleet on every transient stream failure.
		if !readied {
			readied = true
			if onReady != nil {
				onReady(ctx)
			}
		}

		err = n.consume(ctx, stream, handle, &resumeToken)
		n.closeStream(stream)

		if ctx.Err() != nil {
			return
		}
		if isChangeStreamUnsupported(err) {
			n.warnUnsupported(err)
			return
		}
		if isResumeLost(err) && len(resumeToken) > 0 {
			log.Println("mqtt: notifier resume point lost on " + collection + ", restarting from now: " + err.Error())
			resumeToken = nil
			continue
		}
		if err != nil {
			log.Println("mqtt: notifier stream on " + collection + " ended: " + err.Error())
		}

		if time.Since(openedAt) >= notifierBackoffReset {
			backoff = notifierBackoffInitial
		}
		if !waitBackoff(ctx, backoff) {
			return
		}
		backoff = nextNotifierBackoff(backoff)
	}
}

// openStream starts a change stream, resuming after resumeToken when one is
// held. UpdateLookup is required: without it an update event carries only the
// modified fields, and the notifier needs the document's identity.
func (n *Notifier) openStream(
	ctx context.Context,
	collection string,
	pipeline mongo.Pipeline,
	resumeToken bson.Raw,
) (*mongo.ChangeStream, error) {
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	if len(resumeToken) > 0 {
		opts.SetResumeAfter(resumeToken)
	}

	return n.mongoClient.
		Database(utils.MongoDb).
		Collection(collection).
		Watch(ctx, pipeline, opts)
}

// consume drains a stream until it fails or ctx is cancelled, advancing
// *resumeToken as events are handled so that a restart picks up exactly where
// this stream stopped. resumeToken is owned by the calling watcher goroutine.
func (n *Notifier) consume(
	ctx context.Context,
	stream *mongo.ChangeStream,
	handle func(context.Context, *changeEvent),
	resumeToken *bson.Raw,
) error {
	for stream.Next(ctx) {
		event := &changeEvent{}
		if err := stream.Decode(event); err != nil {
			// A shape we cannot read is not a reason to stop watching;
			// record the token so we do not see it again and move on.
			log.Println("mqtt: notifier cannot decode change event: " + err.Error())
		} else {
			handle(ctx, event)
		}

		if token := stream.ResumeToken(); len(token) > 0 {
			*resumeToken = token
		}
	}

	return stream.Err()
}

// closeStream releases the server side cursor on a context of its own, since
// the watch context is usually already cancelled by the time we get here.
func (n *Notifier) closeStream(stream *mongo.ChangeStream) {
	ctx, cancel := context.WithTimeout(context.Background(), notifierCloseTimeout)
	defer cancel()

	if err := stream.Close(ctx); err != nil {
		log.Println("mqtt: notifier could not close change stream: " + err.Error())
	}
}

// warnUnsupported logs once and lets the caller return cleanly. Change streams
// need a replica set; a deployment without one keeps working over REST polling.
func (n *Notifier) warnUnsupported(err error) {
	n.unsupportedOnce.Do(func() {
		log.Println("mqtt: change streams unavailable on this mongo deployment, " +
			"devices will not receive push notifications: " + err.Error())
	})
}

// stepNotice is the payload published on the steps/new topic. It is a summary,
// not the step: the state blurb travels over HTTPS as it always has.
type stepNotice struct {
	Rev         int       `json:"rev"`
	StateSha    string    `json:"state-sha"`
	CommitMsg   string    `json:"commit-msg"`
	Status      string    `json:"status"`
	TimeCreated time.Time `json:"time-created"`
}

// handleStepChange republishes the pending step for whichever trail the changed
// step belongs to. The changed document itself is deliberately not used as the
// payload: a step may be inserted, or marked DONE, out of revision order, and
// the device must always be pointed at the oldest outstanding revision.
func (n *Notifier) handleStepChange(ctx context.Context, event *changeEvent) {
	trailID, ok := stepTrailID(event)
	if !ok {
		log.Println("mqtt: notifier could not determine trail for step change")
		return
	}

	n.publishOldestNewStep(ctx, trailID)
}

// stepTrailID recovers the trail (device) a step change belongs to.
func stepTrailID(event *changeEvent) (primitive.ObjectID, bool) {
	if value, err := event.FullDocument.LookupErr("trail-id"); err == nil {
		if id, ok := value.ObjectIDOK(); ok {
			return id, true
		}
	}

	// Fallback for events without a full document: a step _id is
	// "<trail-id hex>-<rev>", and the hex never contains a dash.
	value, err := event.DocumentKey.LookupErr("_id")
	if err != nil {
		return primitive.NilObjectID, false
	}

	raw, ok := value.StringValueOK()
	if !ok {
		return primitive.NilObjectID, false
	}

	hex, _, found := strings.Cut(raw, "-")
	if !found {
		return primitive.NilObjectID, false
	}

	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return primitive.NilObjectID, false
	}

	return id, true
}

// publishOldestNewStep publishes the step a device polling
// GET /trails/<id>/steps would be handed right now.
//
// The query mirrors trails.handleGetSteps for a DEVICE caller: status NEW, not
// garbage, sorted by rev ascending, first hit only. Publishing the newest step
// instead would make a device that is several revisions behind skip the
// revisions in between. The handler's additional "device" predicate is an
// authorization scope and is redundant here, since a trail-id already selects a
// single device's steps.
func (n *Notifier) publishOldestNewStep(ctx context.Context, trailID primitive.ObjectID) {
	queryCtx, cancel := context.WithTimeout(ctx, notifierQueryTimeout)
	defer cancel()

	query := bson.M{
		"trail-id":        trailID,
		"progress.status": "NEW",
		"garbage":         bson.M{"$ne": true},
	}

	step := trailmodels.Step{}
	err := n.mongoClient.
		Database(utils.MongoDb).
		Collection(stepsCollection).
		FindOne(queryCtx, query, options.FindOne().SetSort(bson.M{"rev": 1})).
		Decode(&step)

	topic := Topic(trailID.Hex(), SuffixStepsNew)

	if errors.Is(err, mongo.ErrNoDocuments) {
		// A zero length retained publish is how MQTT clears retention:
		// mochi deletes the retained packet for the topic instead of
		// storing it, so a device subscribing later is told nothing is
		// pending rather than handed a stale revision.
		n.publish(topic, nil)
		return
	}
	if err != nil {
		log.Println("mqtt: notifier could not load pending step for trail " + trailID.Hex() + ": " + err.Error())
		return
	}

	payload, err := json.Marshal(stepNotice{
		Rev:         step.Rev,
		StateSha:    step.StateSha,
		CommitMsg:   step.CommitMsg,
		Status:      step.StepProgress.Status,
		TimeCreated: step.TimeCreated,
	})
	if err != nil {
		log.Println("mqtt: notifier could not encode step notice: " + err.Error())
		return
	}

	n.publish(topic, payload)
}

// handleDeviceChange republishes user-meta when, and only when, user-meta was
// what changed. Every other field of a device document — the shared secret
// above all — stays on the Hub.
func (n *Notifier) handleDeviceChange(ctx context.Context, event *changeEvent) {
	// Only an update carries an update description; a replace or an insert
	// says nothing about which field moved and must not leak the document.
	if event.OperationType != "update" || !userMetaChanged(event) {
		return
	}

	deviceID, ok := changeDeviceID(event)
	if !ok {
		log.Println("mqtt: notifier could not determine device for user-meta change")
		return
	}

	// The full document is the post-image; when it is missing the device was
	// removed between the change and the lookup and there is nothing to send.
	value, err := event.FullDocument.LookupErr("user-meta")
	if err != nil {
		return
	}

	userMeta := map[string]interface{}{}
	if err := value.Unmarshal(&userMeta); err != nil {
		log.Println("mqtt: notifier could not decode user-meta of device " + deviceID + ": " + err.Error())
		return
	}

	// Meta maps are stored BSON quoted (dots and dollars replaced by
	// sentinels); devices must see the keys they wrote.
	payload, err := json.Marshal(utils.BsonUnquoteMap(&userMeta))
	if err != nil {
		log.Println("mqtt: notifier could not encode user-meta of device " + deviceID + ": " + err.Error())
		return
	}

	n.publish(Topic(deviceID, SuffixUserMeta), payload)
}

// userMetaChanged reports whether an update touched user-meta.
//
// Both whole map writes ($set of "user-meta") and per key writes ($set/$unset
// of "user-meta.<quoted key>", which is what the PATCH handler emits) count,
// and nothing else does.
func userMetaChanged(event *changeEvent) bool {
	elements, err := event.UpdateDescription.UpdatedFields.Elements()
	if err == nil {
		for _, element := range elements {
			key, err := element.KeyErr()
			if err == nil && isUserMetaPath(key) {
				return true
			}
		}
	}

	for _, key := range event.UpdateDescription.RemovedFields {
		if isUserMetaPath(key) {
			return true
		}
	}

	return false
}

// isUserMetaPath matches the user-meta field itself and any path below it,
// without matching a sibling field that merely starts with the same characters.
func isUserMetaPath(key string) bool {
	return key == "user-meta" || strings.HasPrefix(key, "user-meta.")
}

// changeDeviceID returns the hex device id an event refers to.
func changeDeviceID(event *changeEvent) (string, bool) {
	for _, raw := range []bson.Raw{event.FullDocument, event.DocumentKey} {
		value, err := raw.LookupErr("_id")
		if err != nil {
			continue
		}
		if id, ok := value.ObjectIDOK(); ok {
			return id.Hex(), true
		}
	}

	return "", false
}

// publish sends a retained notification. A nil or empty payload clears the
// retained message for the topic. A momentary failure is retried, because the
// change stream that produced this event will not redeliver it and the resume
// token has effectively moved past it.
func (n *Notifier) publish(topic string, payload []byte) {
	var err error
	for attempt := 0; attempt < notifierPublishAttempts; attempt++ {
		if err = n.server.Publish(topic, payload, true, notifierQoS); err == nil {
			return
		}
		time.Sleep(notifierPublishRetryDelay)
	}
	log.Println("mqtt: notifier could not publish to " + topic + " after retries: " + err.Error())
}

// isChangeStreamUnsupported reports whether the deployment can never serve
// change streams, as opposed to having failed to serve one right now.
func isChangeStreamUnsupported(err error) bool {
	if err == nil {
		return false
	}

	if hasServerErrorCode(err, notifierUnsupportedCodes) {
		return true
	}

	// Older servers and some proxies only say so in the message.
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "only supported on replica sets") ||
		strings.Contains(message, "$changestream is not supported") ||
		strings.Contains(message, "unrecognized pipeline stage name: $changestream")
}

// isResumeLost reports whether the held resume token can no longer be used.
func isResumeLost(err error) bool {
	if err == nil {
		return false
	}

	if hasServerErrorCode(err, notifierResumeLostCodes) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "resume of change stream was not possible") ||
		strings.Contains(message, "resume token")
}

func hasServerErrorCode(err error, codes []int) bool {
	var serverErr mongo.ServerError
	if !errors.As(err, &serverErr) {
		return false
	}

	for _, code := range codes {
		if serverErr.HasErrorCode(code) {
			return true
		}
	}

	return false
}

func nextNotifierBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > notifierBackoffMax {
		return notifierBackoffMax
	}
	return next
}

// waitBackoff waits for d, reporting false when ctx was cancelled first so
// callers can return without another round trip.
func waitBackoff(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
