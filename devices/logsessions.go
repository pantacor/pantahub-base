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

package devices

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Live device logs: an owner opens a session, the Hub asks the device (over
// MQTT, see the notifier) to stream the selected log files, the device
// publishes batches that the MQTT bridge stores for a few minutes, and the
// owner reads them back with a long poll. Nothing streams forever: a session
// has a lease the client renews while it watches and a hard deadline. The wire
// contract is docs/logs.md in pv-mqttsdk; field names and values follow it.
//
// Live batches are not the persisted logs topic: they never reach the logs
// service or Elasticsearch, and are dropped 10 minutes after they arrive.

const (
	// LogSessionsCollection holds one document per session: the audit trail
	// of who streamed what from which device, and the state the API, the
	// notifier and the bridge share.
	LogSessionsCollection = "pantahub_log_sessions"

	// LogStreamCollection holds the batches a device streamed, for
	// LogStreamRetention.
	LogStreamCollection = "pantahub_log_stream"
	// LogSessionLimitsCollection holds each device's and each owner's recent
	// session starts, for the start rate limits ({_id: device id, or
	// logOwnerLimitKey(owner), sent: [...], updated_at}). The two kinds of
	// _id cannot collide: an ObjectID and a string.
	LogSessionLimitsCollection = "pantahub_log_session_limits"
)

const (
	// LogSessionLease is how long a session streams without a renewal. The
	// client renews every 20 seconds while it watches; a closed tab stops
	// renewing and the session ends within a lease.
	LogSessionLease = 60 * time.Second

	// LogSessionMaxDuration is the hard limit of a session, renewals or not.
	LogSessionMaxDuration = 30 * time.Minute

	// LogSessionGrace is how long past its end a session still accepts
	// batches: the device's last batch (its "end") is sent after the device
	// saw the lease run out or the stop arrive, so it lands a little late.
	LogSessionGrace = 10 * time.Second

	// LogStreamRetention is how long a batch is kept.
	LogStreamRetention = 10 * time.Minute

	// LogSessionRetention is how long a session document is kept, as the
	// record of who streamed what; it holds no log lines.
	LogSessionRetention = 30 * 24 * time.Hour

	// MaxLogSessionsPerDevice and MaxLogSessionsPerUser cap live sessions.
	MaxLogSessionsPerDevice = 2
	MaxLogSessionsPerUser   = 5

	// A device's sessions can be started at most logStartRateLimit times per
	// logStartRateWindow: the live caps bound concurrent sessions, not a
	// start/stop loop, and each start costs a publish on every replica.
	logStartRateLimit  = 10
	logStartRateWindow = time.Minute
	logLimitsIdle      = time.Hour

	// An owner's sessions, across all its devices, can be started at most
	// logOwnerStartRateLimit times per logStartRateWindow: the per-device
	// limit alone lets an owner of N devices loop 10*N starts a minute.
	logOwnerStartRateLimit = 30

	// A session stores at most this many batches and bytes (logBatchBytes): a
	// device honouring the contract (64 KiB/s for at most 30 min, a batch
	// every 500 ms or 32 KiB) stays well under both; a device flooding its
	// own session stops being stored.
	MaxLogSessionBatches = 10000
	MaxLogSessionBytes   = 128 << 20

	// A caller may hold at most maxLogPollsPerCaller long-polls on this
	// replica at a time; each one holds a goroutine and Mongo reads for up
	// to logLinesWait. At least one per live session a user may hold (plus
	// one for a poll still unwinding), so a user watching all of them is
	// never refused even when every poll lands on one replica.
	maxLogPollsPerCaller = MaxLogSessionsPerUser + 1

	// MaxLogSources and MaxLogTail bound what one session asks for.
	MaxLogSources = 10
	MaxLogTail    = 500

	maxLogSourceLength = 512
	maxLogRevLength    = 128

	// maxLogSessionRequestSize caps the body of the session endpoints.
	maxLogSessionRequestSize = 8 * 1024

	// logLinesMaxBatches caps the batches one GET .../lines returns; the
	// client asks again for the rest.
	logLinesMaxBatches = 20

	// LogCurrentRev names the running revision.
	LogCurrentRev = "current"
)

// Why a session ended. The Hub ends a session on a stop, a lease that ran out
// or the deadline; the device reports its own reason with its last batch.
const (
	LogEndStopped  = "stopped"
	LogEndExpired  = "expired"
	LogEndDeadline = "deadline"

	// Who ended it: the notifier only tells the device about the Hub's ends.
	LogEndedByHub    = "hub"
	LogEndedByDevice = "device"
)

// Names of the unique partial indexes that cap live sessions (see
// insertLogSession).
const (
	logDeviceSlotIndex = "live_device_slot"
	logUserSlotIndex   = "live_user_slot"
)

// Long-poll timing of GET .../lines; variables so tests can shorten them.
var (
	logLinesWait         = 20 * time.Second
	logLinesPollInterval = 500 * time.Millisecond
)

// LogSessionScopes may open, renew and stop sessions. Streaming a device's
// logs reads everything its containers print, so the general devices scopes
// an OAuth app may hold do not grant it: it takes the full API scope or the
// dedicated devices.logs one.
var LogSessionScopes = []utils.Scope{
	utils.Scopes.API,
	utils.Scopes.DeviceLogs,
}

// readLogSessionScopes may read the lines of a session the caller opened: the
// device read scopes, and devices.logs.
func readLogSessionScopes(readDevicesScopes []utils.Scope) []utils.Scope {
	return append([]utils.Scope{utils.Scopes.DeviceLogs}, readDevicesScopes...)
}

// LogSession is a session document as stored in LogSessionsCollection.
//
// A session is live until it ends. While live it holds one of its device's
// MaxLogSessionsPerDevice slots and one of its owner's MaxLogSessionsPerUser
// slots; unique partial indexes on the live sessions make a slot impossible
// to hold twice.
type LogSession struct {
	ID         primitive.ObjectID `bson:"_id"`
	DeviceID   primitive.ObjectID `bson:"device_id"`
	Owner      string             `bson:"owner"`
	CreatedBy  string             `bson:"created_by"`
	Rev        string             `bson:"rev"`
	Sources    []string           `bson:"sources"`
	Tail       int                `bson:"tail"`
	Follow     bool               `bson:"follow"`
	CreatedAt  time.Time          `bson:"created_at"`
	ExpiresAt  time.Time          `bson:"expires_at"`
	Deadline   time.Time          `bson:"deadline"`
	Live       bool               `bson:"live"`
	DeviceSlot int                `bson:"device_slot"`
	UserSlot   int                `bson:"user_slot"`
	EndedAt    *time.Time         `bson:"ended_at,omitempty"`
	EndedBy    string             `bson:"ended_by,omitempty"`
	Reason     string             `bson:"reason"`
	// StreamBatches and StreamBytes count what the device stored for the
	// session, against MaxLogSessionBatches / MaxLogSessionBytes.
	StreamBatches int64 `bson:"stream_batches"`
	StreamBytes   int64 `bson:"stream_bytes"`
}

// LogSessionRequest is the body of POST /devices/{id}/log-sessions.
type LogSessionRequest struct {
	Rev     string   `json:"rev"`
	Sources []string `json:"sources"`
	Tail    int      `json:"tail"`
	Follow  bool     `json:"follow"`
}

// LogSessionView is a session as the API returns it.
type LogSessionView struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"device_id"`
	Rev       string     `json:"rev"`
	Sources   []string   `json:"sources"`
	Tail      int        `json:"tail"`
	Follow    bool       `json:"follow"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Deadline  time.Time  `json:"deadline"`
	Ended     bool       `json:"ended"`
	EndedAt   *time.Time `json:"ended_at"`
	Reason    string     `json:"reason"`
	CreatedBy string     `json:"created_by"`
}

// View renders a stored session for the API.
func (s *LogSession) View() LogSessionView {
	sources := s.Sources
	if sources == nil {
		sources = []string{}
	}
	return LogSessionView{
		ID:        s.ID.Hex(),
		DeviceID:  s.DeviceID.Hex(),
		Rev:       s.Rev,
		Sources:   sources,
		Tail:      s.Tail,
		Follow:    s.Follow,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		Deadline:  s.Deadline,
		Ended:     !s.Live,
		EndedAt:   s.EndedAt,
		Reason:    s.Reason,
		CreatedBy: s.CreatedBy,
	}
}

// LogLine is one line of a batch: the source it was read from and the text.
type LogLine struct {
	Src  string `json:"src" bson:"src"`
	Line string `json:"line" bson:"line"`
}

// LogBatch is a batch as stored in LogStreamCollection, and as GET .../lines
// returns it (without the internal ids).
type LogBatch struct {
	ID        primitive.ObjectID `json:"-" bson:"_id"`
	Session   primitive.ObjectID `json:"-" bson:"session"`
	DeviceID  primitive.ObjectID `json:"-" bson:"device_id"`
	Seq       int64              `json:"seq" bson:"seq"`
	Lines     []LogLine          `json:"lines" bson:"lines"`
	Dropped   int64              `json:"dropped" bson:"dropped"`
	End       bool               `json:"end" bson:"end"`
	Reason    string             `json:"reason" bson:"reason"`
	CreatedAt time.Time          `json:"created_at" bson:"created_at"`
}

// LogLinesView is the answer of GET .../lines. next is the seq to ask after
// next time; ended is set once the session has ended and every batch it
// stored has been returned.
type LogLinesView struct {
	Batches []LogBatch `json:"batches"`
	Next    int64      `json:"next"`
	Ended   bool       `json:"ended"`
	Reason  string     `json:"reason"`
}

// ValidateLogRev checks a revision name: "current" or a revision name such as
// "locals/hub-3" or "12". It becomes a path on the device
// (/pantavisor/logs/<rev>), so it may not climb out of it or be absolute.
func ValidateLogRev(rev string) error {
	switch {
	case rev == "":
		return errors.New("rev must not be empty")
	case len(rev) > maxLogRevLength:
		return errors.New("rev is longer than " + strconv.Itoa(maxLogRevLength) + " bytes")
	case strings.HasPrefix(rev, "/"):
		return errors.New("rev must not start with /")
	case strings.Contains(rev, ".."):
		return errors.New("rev must not contain ..")
	case strings.HasSuffix(rev, "/") || strings.Contains(rev, "//"):
		return errors.New("rev has an empty path element")
	case strings.ContainsFunc(rev, notPathSafe):
		return errors.New("rev contains a character that is not allowed")
	}
	return nil
}

// ValidateLogSource checks a source: a path relative to the revision's log
// directory ("" is the whole revision, a directory everything under it). It
// may not be absolute or have a ".." element; the device checks again that
// the resolved file stays inside the revision.
func ValidateLogSource(source string) error {
	if len(source) > maxLogSourceLength {
		return errors.New("source is longer than " + strconv.Itoa(maxLogSourceLength) + " bytes")
	}
	if strings.HasPrefix(source, "/") {
		return errors.New("source " + strconv.Quote(source) + " is an absolute path")
	}
	if strings.ContainsFunc(source, notPathSafe) {
		return errors.New("source " + strconv.Quote(source) + " contains a character that is not allowed")
	}
	for _, element := range strings.Split(source, "/") {
		if element == ".." {
			return errors.New("source " + strconv.Quote(source) + " contains ..")
		}
	}
	return nil
}

// notPathSafe refuses control characters, backslashes and invalid UTF-8 in a
// path taken from a request.
func notPathSafe(r rune) bool {
	return r == '\\' || r == unicode.ReplacementChar || unicode.IsControl(r)
}

// ValidateLogSessionRequest checks a request and returns it normalised: the
// revision defaults to "current", duplicate sources are dropped.
func ValidateLogSessionRequest(req LogSessionRequest) (LogSessionRequest, error) {
	if req.Rev == "" {
		req.Rev = LogCurrentRev
	}
	if err := ValidateLogRev(req.Rev); err != nil {
		return req, err
	}

	if len(req.Sources) == 0 {
		return req, errors.New("sources must name at least one source")
	}
	if len(req.Sources) > MaxLogSources {
		return req, errors.New("at most " + strconv.Itoa(MaxLogSources) + " sources per session")
	}
	seen := map[string]bool{}
	sources := make([]string, 0, len(req.Sources))
	for _, source := range req.Sources {
		if err := ValidateLogSource(source); err != nil {
			return req, err
		}
		if !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	req.Sources = sources

	if req.Tail < 0 || req.Tail > MaxLogTail {
		return req, errors.New("tail must be between 0 and " + strconv.Itoa(MaxLogTail))
	}
	return req, nil
}

var (
	errLogDeviceLimit = errors.New("this device already streams " + strconv.Itoa(MaxLogSessionsPerDevice) +
		" live log sessions, stop one or try again when one ends")
	errLogUserLimit = errors.New("you already have " + strconv.Itoa(MaxLogSessionsPerUser) +
		" live log sessions, stop one or try again when one ends")
)

// insertLogSession stores a new live session in a free device slot and a free
// user slot, or refuses with errLogDeviceLimit / errLogUserLimit. The slots
// are unique among live sessions (logDeviceSlotIndex, logUserSlotIndex), so
// concurrent requests, on any replica, can never hold more than the limits
// together: the insert that finds its slot taken fails with a duplicate key
// and tries the next one.
func (a *App) insertLogSession(ctx context.Context, session *LogSession) error {
	sessions := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection)

	deviceSlot, userSlot := 0, 0
	for deviceSlot < MaxLogSessionsPerDevice && userSlot < MaxLogSessionsPerUser {
		session.DeviceSlot, session.UserSlot = deviceSlot, userSlot
		_, err := sessions.InsertOne(ctx, session)
		if err == nil {
			return nil
		}
		switch takenLogSlot(err) {
		case logDeviceSlotIndex:
			deviceSlot++
		case logUserSlotIndex:
			userSlot++
		default:
			return err
		}
	}
	if deviceSlot >= MaxLogSessionsPerDevice {
		return errLogDeviceLimit
	}
	return errLogUserLimit
}

// takenLogSlot names the slot index a duplicate key error is about, or "".
func takenLogSlot(err error) string {
	if !mongo.IsDuplicateKeyError(err) {
		return ""
	}
	message := err.Error()
	for _, index := range []string{logDeviceSlotIndex, logUserSlotIndex} {
		if strings.Contains(message, index) {
			return index
		}
	}
	return ""
}

// endStaleLogSessionsUpdate ends every live session whose lease has run out,
// with reason "deadline" when the hard limit is what ran out. The lease never
// runs past the deadline, so an expired lease covers both.
func endStaleLogSessionsUpdate(now time.Time) mongo.Pipeline {
	return mongo.Pipeline{{{Key: "$set", Value: bson.M{
		"live":     false,
		"ended_at": now,
		"ended_by": LogEndedByHub,
		"reason": bson.M{"$cond": bson.A{
			bson.M{"$lte": bson.A{"$deadline", now}},
			LogEndDeadline,
			LogEndExpired,
		}},
	}}}}
}

// EndStaleLogSessions ends the live sessions matching filter whose lease or
// deadline has passed. The API calls it lazily for the sessions it is about
// to count or read; the MQTT service sweeps all of them periodically
// (RunLogSessionSweep), so a closed browser tab frees its slot even when
// nobody asks.
func EndStaleLogSessions(ctx context.Context, client *mongo.Client, filter bson.M, now time.Time) error {
	match := bson.M{"live": true, "expires_at": bson.M{"$lte": now}}
	for key, value := range filter {
		match[key] = value
	}
	_, err := client.Database(utils.MongoDb).Collection(LogSessionsCollection).
		UpdateMany(ctx, match, endStaleLogSessionsUpdate(now))
	return err
}

// LogSessionSweepInterval is how often RunLogSessionSweep runs.
const LogSessionSweepInterval = 15 * time.Second

// RunLogSessionSweep ends stale sessions every LogSessionSweepInterval until
// ctx is cancelled. Every replica may run it: ending is idempotent.
func RunLogSessionSweep(ctx context.Context, client *mongo.Client) {
	ticker := time.NewTicker(LogSessionSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := EndStaleLogSessions(sweepCtx, client, nil, time.Now().UTC()); err != nil && ctx.Err() == nil {
				log.Println("devices: cannot end stale log sessions: " + err.Error())
			}
			cancel()
		}
	}
}

// handlePostLogSession opens a live log session on a device
// @Summary Stream live logs from a device connected over MQTT
// @Description Asks the device to stream the selected log sources of a revision ("current" by default).
// @Description Sources are paths relative to the revision's log directory; "" is the whole revision.
// @Description At most 10 sources and a tail of 500 lines. The session lasts 60 seconds unless renewed, 30 minutes at most.
// @Description Only the device owner may stream, and only while the device is connected over MQTT.
// @Description Requires the full API scope or devices.logs. At most 2 live sessions per device and 5 per user.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body LogSessionRequest true "Session"
// @Success 201 {object} LogSessionView
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 409 {object} utils.RError
// @Failure 413 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/log-sessions [post]
func (a *App) handlePostLogSession(c *echo.Context) error {
	caller, ok := callerPrn(c)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return userError(c, "Device not found", http.StatusNotFound)
	}

	req := LogSessionRequest{}
	c.Request().Body = http.MaxBytesReader(nil, c.Request().Body, maxLogSessionRequestSize)
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return userError(c, "Log session request is larger than "+strconv.Itoa(maxLogSessionRequestSize)+" bytes", http.StatusRequestEntityTooLarge)
		}
		return userError(c, "Error parsing log session request: "+err.Error(), http.StatusBadRequest)
	}
	req, err = ValidateLogSessionRequest(req)
	if err != nil {
		return userError(c, err.Error(), http.StatusBadRequest)
	}

	connected, err := a.mqttConnected(c.Request().Context(), device.DeviceMeta)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking the MQTT connection: "+err.Error(), http.StatusInternalServerError)
	}
	if !connected {
		return userError(c, "Device is not connected over MQTT", http.StatusConflict)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// Whole seconds: expires_at and deadline go on the wire as RFC 3339
	// without a fraction, and the stored times must read back as returned.
	now := time.Now().UTC().Truncate(time.Second)

	limits := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionLimitsCollection)
	reserved, err := reserveWindowSlot(ctx, limits, device.ID, logStartRateLimit, logStartRateWindow, time.Now().UTC(), nil, nil)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking the log session rate: "+err.Error(), http.StatusInternalServerError)
	}
	if !reserved {
		return userError(c, errLogStartRateLimited.Error(), http.StatusTooManyRequests)
	}
	reserved, err = reserveWindowSlot(ctx, limits, logOwnerLimitKey(device.Owner), logOwnerStartRateLimit, logStartRateWindow, time.Now().UTC(), nil, nil)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking the log session rate: "+err.Error(), http.StatusInternalServerError)
	}
	if !reserved {
		return userError(c, errLogOwnerStartRateLimited.Error(), http.StatusTooManyRequests)
	}

	// Sessions whose tab was closed free their slots here, before counting.
	err = EndStaleLogSessions(ctx, a.mongoClient, bson.M{"$or": bson.A{
		bson.M{"device_id": device.ID},
		bson.M{"owner": device.Owner},
	}}, now)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error ending stale log sessions: "+err.Error(), http.StatusInternalServerError)
	}

	session := LogSession{
		ID:        primitive.NewObjectID(),
		DeviceID:  device.ID,
		Owner:     device.Owner,
		CreatedBy: caller,
		Rev:       req.Rev,
		Sources:   req.Sources,
		Tail:      req.Tail,
		Follow:    req.Follow,
		CreatedAt: now,
		ExpiresAt: now.Add(LogSessionLease),
		Deadline:  now.Add(LogSessionMaxDuration),
		Live:      true,
	}

	// The insert is the start: the MQTT notifier on every replica watches
	// this collection and publishes the start to the device.
	if err := a.insertLogSession(ctx, &session); err != nil {
		if errors.Is(err, errLogDeviceLimit) || errors.Is(err, errLogUserLimit) {
			return userError(c, err.Error(), http.StatusTooManyRequests)
		}
		return echoutil.RestErrorWrapper(c, "Error storing log session: "+err.Error(), http.StatusInternalServerError)
	}

	auditLogSession(&session)

	return echoutil.WriteJSON(c, http.StatusCreated, session.View())
}

// auditLogSession records who started streaming what from which device. The
// session document is the durable record (kept LogSessionRetention); this is
// the line an operator greps for.
func auditLogSession(session *LogSession) {
	log.Printf("devices: audit: log session %s started by %s on device %s (owner %s): rev %s, sources %s, tail %d, follow %t",
		session.ID.Hex(), session.CreatedBy, session.DeviceID.Hex(), session.Owner,
		strconv.Quote(session.Rev), quoteAll(session.Sources), session.Tail, session.Follow)
}

func quoteAll(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return "[" + strings.Join(quoted, " ") + "]"
}

// findLogSession loads a session of a device of the caller, answering
// the errors itself; session is nil when it did.
func (a *App) findLogSession(c *echo.Context) (*LogSession, error) {
	caller, ok := callerPrn(c)
	if !ok {
		return nil, echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	sessionID, err := primitive.ObjectIDFromHex(c.Param("sid"))
	if err != nil {
		return nil, userError(c, "Invalid log session id", http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return nil, echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return nil, userError(c, "Device not found", http.StatusNotFound)
	}

	session, err := a.loadLogSession(c.Request().Context(), sessionID, device.ID, caller)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, userError(c, "Log session not found", http.StatusNotFound)
	}
	if err != nil {
		return nil, echoutil.RestErrorWrapper(c, "Error loading log session: "+err.Error(), http.StatusInternalServerError)
	}
	return session, nil
}

// loadLogSession reads a session of a device and owner, ending it first if
// its lease ran out.
func (a *App) loadLogSession(ctx context.Context, sessionID, deviceID primitive.ObjectID, owner string) (*LogSession, error) {
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": sessionID, "device_id": deviceID, "owner": owner}
	if err := EndStaleLogSessions(ctxC, a.mongoClient, filter, time.Now().UTC()); err != nil {
		return nil, err
	}
	session := LogSession{}
	err := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection).FindOne(ctxC, filter).Decode(&session)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// LogSessionRenewal is the answer of POST .../renew.
type LogSessionRenewal struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
	Deadline  time.Time `json:"deadline"`
}

// handleRenewLogSession renews the lease of a live log session
// @Summary Renew the lease of a live log session
// @Description Extends the session by 60 seconds from now, never past its 30 minute deadline.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "Log session ID"
// @Success 200 {object} LogSessionRenewal
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 410 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/log-sessions/{sid}/renew [post]
func (a *App) handleRenewLogSession(c *echo.Context) error {
	session, err := a.findLogSession(c)
	if session == nil {
		return err
	}
	if !session.Live {
		return userError(c, "Log session has ended ("+session.Reason+")", http.StatusGone)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(LogSessionLease)
	if expiresAt.After(session.Deadline) {
		expiresAt = session.Deadline
	}

	// Only a session still live, and whose lease has not run out meanwhile,
	// is renewed; the notifier sees the new expires_at and tells the device.
	result, err := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection).UpdateOne(ctx,
		bson.M{"_id": session.ID, "live": true, "expires_at": bson.M{"$gt": time.Now().UTC()}},
		bson.M{"$set": bson.M{"expires_at": expiresAt}})
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error renewing log session: "+err.Error(), http.StatusInternalServerError)
	}
	if result.MatchedCount == 0 {
		return userError(c, "Log session has ended", http.StatusGone)
	}

	return echoutil.WriteJSON(c, http.StatusOK, LogSessionRenewal{
		ID:        session.ID.Hex(),
		ExpiresAt: expiresAt,
		Deadline:  session.Deadline,
	})
}

// handleDeleteLogSession stops a log session
// @Summary Stop a log session
// @Description The device is told to stop streaming. Stopping a session that has already ended does nothing.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "Log session ID"
// @Success 204
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/log-sessions/{sid} [delete]
func (a *App) handleDeleteLogSession(c *echo.Context) error {
	session, err := a.findLogSession(c)
	if session == nil {
		return err
	}

	if session.Live {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()
		// The notifier sees the end and tells the device to stop.
		_, err := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection).UpdateOne(ctx,
			bson.M{"_id": session.ID, "live": true},
			bson.M{"$set": bson.M{
				"live":     false,
				"ended_at": time.Now().UTC(),
				"ended_by": LogEndedByHub,
				"reason":   LogEndStopped,
			}})
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error stopping log session: "+err.Error(), http.StatusInternalServerError)
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// parseLogSeq reads ?after: absent is -1 (from the first batch).
func parseLogSeq(raw string) (int64, error) {
	if raw == "" {
		return -1, nil
	}
	seq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seq < -1 {
		return 0, errors.New("after must be an integer, -1 or more")
	}
	return seq, nil
}

// handleGetLogLines long-polls the batches of a log session
// @Summary Read the lines a log session streamed
// @Description Waits up to 20 seconds for batches with a seq above ?after (default -1: from the first),
// @Description and returns as soon as there are some or the session has ended. Batches are kept 10 minutes.
// @Description next is the seq to pass as ?after next time; ended is set once the session has ended and every batch was returned.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "Log session ID"
// @Param after query int false "Return batches after this seq"
// @Success 200 {object} LogLinesView
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/log-sessions/{sid}/lines [get]
func (a *App) handleGetLogLines(c *echo.Context) error {
	after, err := parseLogSeq(c.QueryParam("after"))
	if err != nil {
		return userError(c, err.Error(), http.StatusBadRequest)
	}
	caller, ok := callerPrn(c)
	if !ok {
		return userError(c, "Unknown caller", http.StatusUnauthorized)
	}
	if !a.logPolls.acquire(caller) {
		return userError(c, errLogPollsBusy.Error(), http.StatusTooManyRequests)
	}
	defer a.logPolls.release(caller)

	session, err := a.findLogSession(c)
	if session == nil {
		return err
	}

	ctx := c.Request().Context()
	waitUntil := time.Now().Add(logLinesWait)
	for {
		view, err := a.readLogLines(ctx, session, after)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return echoutil.RestErrorWrapper(c, "Error reading log lines: "+err.Error(), http.StatusInternalServerError)
		}
		if len(view.Batches) > 0 || view.Ended || !time.Now().Before(waitUntil) {
			return echoutil.WriteJSON(c, http.StatusOK, view)
		}

		timer := time.NewTimer(logLinesPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		// The session may have ended meanwhile (stopped, expired, or the
		// device's last batch).
		session, err = a.loadLogSession(ctx, session.ID, session.DeviceID, session.Owner)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return echoutil.RestErrorWrapper(c, "Error loading log session: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// readLogLines reads the batches of a session after a seq, one page.
func (a *App) readLogLines(ctx context.Context, session *LogSession, after int64) (LogLinesView, error) {
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := a.mongoClient.Database(utils.MongoDb).Collection(LogStreamCollection).Find(ctxC,
		bson.M{"session": session.ID, "seq": bson.M{"$gt": after}},
		options.Find().SetSort(bson.D{{Key: "seq", Value: 1}}).SetLimit(logLinesMaxBatches))
	if err != nil {
		return LogLinesView{}, err
	}
	batches := []LogBatch{}
	if err := cursor.All(ctxC, &batches); err != nil {
		return LogLinesView{}, err
	}

	view := LogLinesView{Batches: batches, Next: after}
	for i := range batches {
		if batches[i].Lines == nil {
			batches[i].Lines = []LogLine{}
		}
		view.Next = batches[i].Seq
	}
	// Ended only once nothing is left to read: a full page may have more.
	if !session.Live && len(batches) < logLinesMaxBatches {
		view.Ended = true
		view.Reason = session.Reason
	}
	return view, nil
}

// DeleteDeviceLogSessions removes the log sessions and batches of a device
// that is being deleted.
func (a *App) DeleteDeviceLogSessions(ctx context.Context, deviceID primitive.ObjectID) error {
	db := a.mongoClient.Database(utils.MongoDb)
	if _, err := db.Collection(LogSessionsCollection).DeleteMany(ctx, bson.M{"device_id": deviceID}); err != nil {
		return err
	}
	if _, err := db.Collection(LogStreamCollection).DeleteMany(ctx, bson.M{"device_id": deviceID}); err != nil {
		return err
	}
	_, err := db.Collection(LogSessionLimitsCollection).DeleteOne(ctx, bson.M{"_id": deviceID})
	return err
}

// logOwnerLimitKey is the LogSessionLimitsCollection _id of an owner's start
// rate limit.
func logOwnerLimitKey(owner string) string {
	return "owner:" + owner
}

var (
	errLogStartRateLimited      = errors.New("too many log sessions started for this device, try again in a minute")
	errLogOwnerStartRateLimited = errors.New("too many log sessions started, try again in a minute")
	errLogPollsBusy             = errors.New("too many log reads in progress, try again")
)

// pollSlots counts the long-polls each caller holds on this replica.
type pollSlots struct {
	mu sync.Mutex
	n  map[string]int
}

func (p *pollSlots) acquire(caller string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n == nil {
		p.n = map[string]int{}
	}
	if p.n[caller] >= maxLogPollsPerCaller {
		return false
	}
	p.n[caller]++
	return true
}

func (p *pollSlots) release(caller string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n[caller] <= 1 {
		delete(p.n, caller)
		return
	}
	p.n[caller]--
}

// EnsureLogSessionIndices creates the indexes of the log sessions and batches:
// the unique slots that cap live sessions per device and per user (and count
// them), the one the stale-session sweep reads, the retention TTLs, and the
// unique (session, seq) index batches are read by (a batch the device sends
// twice is stored once).
func (a *App) EnsureLogSessionIndices() error {
	ctx, cancel := context.WithTimeout(context.Background(), CreateIndexTimeout)
	defer cancel()

	live := bson.M{"live": true}
	sessions := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection)
	_, err := sessions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "device_id", Value: int32(1)}, {Key: "device_slot", Value: int32(1)}},
			Options: options.Index().SetName(logDeviceSlotIndex).SetUnique(true).SetPartialFilterExpression(live),
		},
		{
			Keys:    bson.D{{Key: "owner", Value: int32(1)}, {Key: "user_slot", Value: int32(1)}},
			Options: options.Index().SetName(logUserSlotIndex).SetUnique(true).SetPartialFilterExpression(live),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: int32(1)}},
			Options: options.Index().SetName("live_expires_at").SetPartialFilterExpression(live),
		},
		{Keys: bson.D{{Key: "device_id", Value: int32(1)}}},
		{
			Keys:    bson.D{{Key: "created_at", Value: int32(1)}},
			Options: options.Index().SetExpireAfterSeconds(int32(LogSessionRetention.Seconds())),
		},
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	if err != nil {
		return err
	}

	limits := a.mongoClient.Database(utils.MongoDb).Collection(LogSessionLimitsCollection)
	_, err = limits.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "updated_at", Value: int32(1)}},
		Options: options.Index().SetExpireAfterSeconds(int32(logLimitsIdle.Seconds())),
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	if err != nil {
		return err
	}

	stream := a.mongoClient.Database(utils.MongoDb).Collection(LogStreamCollection)
	_, err = stream.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "session", Value: int32(1)}, {Key: "seq", Value: int32(1)}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "device_id", Value: int32(1)}}},
		{
			Keys:    bson.D{{Key: "created_at", Value: int32(1)}},
			Options: options.Index().SetExpireAfterSeconds(int32(LogStreamRetention.Seconds())),
		},
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	return err
}
