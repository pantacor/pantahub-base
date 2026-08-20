package mqtt

import (
	"context"
	"encoding/json"
	"log"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

// requestHook answers device pull requests over MQTT so a device can seed
// itself on connect without a REST round-trip. It intercepts publishes to the
// request topics (SuffixUserMetaGet, SuffixStepsGet) and has the notifier
// publish the answer on the topic the device already subscribes to
// (SuffixUserMeta, SuffixStepsNew).
//
// It is separate from bridgeHook -- which persists device *reports* -- because a
// request is a read followed by a publish, not a write, and needs the notifier's
// broker handle to reply. Requests are answered off the broker's read loop, and
// concurrency is bounded so a misbehaving device cannot spawn goroutines without
// limit; the ACL already confines every device to its own namespace.
type requestHook struct {
	mochi.HookBase
	notifier *Notifier
	sem      chan struct{}
}

const (
	// maxConcurrentRequests caps in-flight request handlers across the fleet.
	// A saturated hook drops the request; the device retries and the retained
	// topics still carry current state, so nothing is lost for long.
	maxConcurrentRequests = 64

	// requestHandleTimeout bounds one request's Mongo work so a slow query
	// cannot hold a worker indefinitely.
	requestHandleTimeout = 15 * time.Second
)

func newRequestHook(notifier *Notifier) *requestHook {
	return &requestHook{
		notifier: notifier,
		sem:      make(chan struct{}, maxConcurrentRequests),
	}
}

// ID identifies the hook to the broker.
func (h *requestHook) ID() string { return "pantahub-request" }

// Provides declares the single hook point this implements.
func (h *requestHook) Provides(b byte) bool {
	return b == mochi.OnPublish
}

// stepsGetRequest is the payload of a steps/get request. An absent or empty
// payload means "since revision 0" -- the whole trail.
type stepsGetRequest struct {
	Since int `json:"since"`
}

// OnPublish intercepts the request topics and answers them asynchronously. The
// packet is returned unchanged: letting the publish flow through keeps the ACL
// path and delivery unchanged, and the request topics carry no retained state.
func (h *requestHook) OnPublish(cl *mochi.Client, pk packets.Packet) (packets.Packet, error) {
	deviceID, suffix, ok := Parse(pk.TopicName)
	if !ok {
		return pk, nil
	}

	switch suffix {
	case SuffixUserMetaGet:
		h.run(func() { h.answerUserMeta(deviceID) })
	case SuffixStepsGet:
		// Copy the payload: mochi reuses the packet buffer after this returns.
		payload := append([]byte(nil), pk.Payload...)
		h.run(func() { h.answerSteps(deviceID, payload) })
	}
	return pk, nil
}

// run executes fn on a bounded worker, dropping the request if saturated.
func (h *requestHook) run(fn func()) {
	select {
	case h.sem <- struct{}{}:
		go func() {
			defer func() {
				<-h.sem
				if r := recover(); r != nil {
					log.Printf("mqtt: request handler panicked: %v", r)
				}
			}()
			fn()
		}()
	default:
		log.Println("mqtt: request hook saturated, dropping a get request")
	}
}

func (h *requestHook) answerUserMeta(deviceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), requestHandleTimeout)
	defer cancel()
	h.notifier.PublishUserMeta(ctx, deviceID)
}

func (h *requestHook) answerSteps(deviceID string, payload []byte) {
	var req stepsGetRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			log.Println("mqtt: steps/get bad payload from " + deviceID + ": " + err.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestHandleTimeout)
	defer cancel()
	h.notifier.PublishStepsSince(ctx, deviceID, req.Since)
}
