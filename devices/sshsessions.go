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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/ssh"
)

// Web SSH: an owner opens a session on a device, the Hub asks the device (over
// MQTT, see the notifier) to accept the browser's public key and to connect to
// its own SSH server, and the encrypted SSH bytes travel between the browser's
// WebSocket and the device's MQTT connection through a TTL collection of
// frames. The Hub never holds anything that can log in to a device: the
// browser keeps the private key, and only the SSH handshake proves it. The
// wire contract is docs/ssh.md in pv-mqttsdk; field names and values follow
// it.
//
// Frames between replicas: the replica serving the WebSocket stores the
// browser's bytes as "up" frames and forwards the device's "down" frames
// (sshws.go); the MQTT bridge stores the "down" frames a device publishes
// (sshframes.go); the replica holding the device's MQTT connection publishes
// the "up" frames (the notifier, with a change stream on the frames).

const (
	// SSHSessionsCollection holds one document per session: the audit trail
	// of who opened a terminal on which device, and the state the API, the
	// notifier and the bridge share.
	SSHSessionsCollection = "pantahub_ssh_sessions"

	// SSHFramesCollection holds the frames of both directions, for
	// SSHFrameRetention.
	SSHFramesCollection = "pantahub_ssh_frames"

	// SSHSessionLimitsCollection holds each device's and each owner's recent
	// session starts, for the start rate limits (as LogSessionLimitsCollection).
	SSHSessionLimitsCollection = "pantahub_ssh_session_limits"
)

const (
	// SSHSessionLease is how long a session lasts without a renewal. The
	// client renews every 20 seconds while the tab is open.
	SSHSessionLease = 60 * time.Second

	// SSHSessionMaxDuration is the hard limit of a session, renewals or not.
	SSHSessionMaxDuration = 60 * time.Minute

	// SSHSessionIdle ends a session without a byte in either direction for
	// this long.
	SSHSessionIdle = 15 * time.Minute

	// SSHSessionGrace is how long past its end a session still takes the
	// device's frames: its last frame (its "end") is sent after the device
	// saw the stop, so it lands a little late.
	SSHSessionGrace = 10 * time.Second

	// SSHFrameRetention is how long a frame is kept at most. A frame is
	// deleted once it is forwarded (a down frame once the WebSocket wrote
	// it, an up frame once the replica holding the device published it),
	// and every frame of a session when the session ends and when its
	// WebSocket closes; the TTL only catches the frames nobody forwarded: a
	// device connected nowhere, a frame landing after its session ended.
	// Past SSHGapTimeout a missing frame has ended its session anyway;
	// Mongo's TTL monitor runs once a minute, so a frame lasts one to two
	// minutes at most. In production the index may still carry the expiry
	// it was created with (ensureTTLIndex cannot always change it): the
	// WARNING it logs names the command an operator runs once.
	SSHFrameRetention = time.Minute

	// SSHSessionRetention is how long a session document is kept, as the
	// record of who opened a terminal where; it holds no bytes.
	SSHSessionRetention = 30 * 24 * time.Hour

	// MaxSSHSessionsPerDevice and MaxSSHSessionsPerUser cap live sessions.
	MaxSSHSessionsPerDevice = 2
	MaxSSHSessionsPerUser   = 3

	// Start rate limits, per device and per owner, as for log sessions.
	sshStartRateLimit      = 10
	sshOwnerStartRateLimit = 30
	sshStartRateWindow     = time.Minute
	sshLimitsIdle          = time.Hour

	// MaxSSHFrameData caps the bytes of one frame, either direction.
	MaxSSHFrameData = 32 * 1024

	// SSHRate is the most bytes a second a session carries in each
	// direction.
	SSHRate = 256 * 1024

	// The Hub checks the device's rate over sshRateWindow, allowing the
	// window's worth of SSHRate plus one full frame: frames sent evenly can
	// still arrive bunched after a network stall. The window also caps the
	// frames, so a device cannot cost a write per byte.
	sshRateWindow       = 4 * time.Second
	sshRateWindowBytes  = int64(SSHRate*4 + MaxSSHFrameData)
	sshRateWindowFrames = 1024

	// SSHUnattachedBytes is the most a device may send while no WebSocket
	// is attached to the session: the SSH banner and key exchange, which the
	// device sends before the browser's first byte, fit; a device streaming
	// on with nobody to take the frames has its session ended, so that the
	// frames of a session with a live lease but no client cannot pile up
	// until the TTL. The count starts again from 0 once a WebSocket attaches.
	SSHUnattachedBytes = int64(64 * 1024)

	// SSHGapTimeout ends a session whose next frame has not arrived this
	// long after a later one did: SSH cannot survive a lost byte.
	SSHGapTimeout = 10 * time.Second

	// SSHTicketLifetime is how long a WebSocket ticket lives: the client
	// asks for it right before it dials.
	SSHTicketLifetime = 30 * time.Second

	// sshTicketBytes is the entropy of a ticket; base64url without padding
	// makes 43 characters of it.
	sshTicketBytes = 32

	// maxSSHSessionRequestSize caps the body of the session endpoints.
	maxSSHSessionRequestSize = 8 * 1024

	maxSSHNameLength = 64
	maxSSHKeyLength  = 1024
)

// SSHTargetHost is the target of the Pantavisor host.
const SSHTargetHost = "_pv_"

// Directions of a frame.
const (
	SSHDirUp   = "up"   // browser -> device
	SSHDirDown = "down" // device -> browser
)

// Why a session ended. The Hub ends a session on a stop, a lease that ran
// out, the deadline, an idle quarter of an hour, a lost frame or a device
// above the rate; the device reports its own reason with its last frame.
const (
	SSHEndClosed     = "closed"
	SSHEndStopped    = "stopped"
	SSHEndExpired    = "expired"
	SSHEndIdle       = "idle"
	SSHEndDeadline   = "deadline"
	SSHEndLostFrame  = "lost frame"
	SSHEndOverflow   = "overflow"
	SSHEndRate       = "error: rate limit exceeded"
	SSHEndUnattached = "error: device streamed without a client"

	SSHEndedByHub    = "hub"
	SSHEndedByDevice = "device"
)

// SSHSessionScopes may open, renew, stop and connect to sessions. A terminal
// on a device is full control of it, so the general devices scopes do not
// grant it: it takes the full API scope or the dedicated devices.ssh one.
var SSHSessionScopes = []utils.Scope{
	utils.Scopes.API,
	utils.Scopes.DeviceSSH,
}

// SSHSession is a session document as stored in SSHSessionsCollection.
//
// While live it holds one of its device's MaxSSHSessionsPerDevice slots and
// one of its owner's MaxSSHSessionsPerUser slots, unique among the live
// sessions (as log sessions do).
type SSHSession struct {
	ID         primitive.ObjectID `bson:"_id"`
	DeviceID   primitive.ObjectID `bson:"device_id"`
	Owner      string             `bson:"owner"`
	CreatedBy  string             `bson:"created_by"`
	Target     string             `bson:"target"`
	Pubkey     string             `bson:"pubkey"`
	CreatedAt  time.Time          `bson:"created_at"`
	ExpiresAt  time.Time          `bson:"expires_at"`
	Deadline   time.Time          `bson:"deadline"`
	Live       bool               `bson:"live"`
	DeviceSlot int                `bson:"device_slot"`
	UserSlot   int                `bson:"user_slot"`
	EndedAt    *time.Time         `bson:"ended_at,omitempty"`
	EndedBy    string             `bson:"ended_by,omitempty"`
	Reason     string             `bson:"reason"`

	// ActiveAt is the last time a byte went either way, for SSHSessionIdle.
	ActiveAt time.Time `bson:"active_at"`
	// Attached is set while a WebSocket serves the session: one at a time.
	Attached bool `bson:"attached"`
	// TicketHash and TicketExpiresAt are the SHA-256 (hex) and the expiry of
	// the WebSocket ticket issued last, one at a time, until the WebSocket
	// consumes it; the ticket itself is never stored.
	TicketHash      string     `bson:"ticket_hash,omitempty"`
	TicketExpiresAt *time.Time `bson:"ticket_expires_at,omitempty"`
	// EndAudited is set once the end of the session has been audited.
	EndAudited bool `bson:"end_audited"`

	// What each direction carried, for the audit.
	UpBytes    int64 `bson:"up_bytes"`
	UpFrames   int64 `bson:"up_frames"`
	DownBytes  int64 `bson:"down_bytes"`
	DownFrames int64 `bson:"down_frames"`

	// The device's rate over the current sshRateWindow.
	DownWindowAt     time.Time `bson:"down_window_at"`
	DownWindowBytes  int64     `bson:"down_window_bytes"`
	DownWindowFrames int64     `bson:"down_window_frames"`

	// UnattachedBytes is what the device sent since the session was last
	// without a WebSocket, against SSHUnattachedBytes; 0 while attached.
	UnattachedBytes int64 `bson:"unattached_bytes"`
}

// SSHSessionRequest is the body of POST /devices/{id}/ssh-sessions.
type SSHSessionRequest struct {
	Target string `json:"target"`
	Pubkey string `json:"pubkey"`
}

// SSHSessionView is a session as the API returns it.
type SSHSessionView struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	ExpiresAt time.Time `json:"expires_at"`
	Deadline  time.Time `json:"deadline"`
}

// SSHSessionRenewal is the answer of POST .../renew.
type SSHSessionRenewal struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
	Deadline  time.Time `json:"deadline"`
}

// SSHSessionTicket is the answer of POST .../ticket: what the WebSocket
// offers as its "ticket.<ticket>" subprotocol, and until when.
type SSHSessionTicket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
}

// newSSHTicket draws a ticket and returns it with its hash, the only form of
// it that is stored.
func newSSHTicket() (ticket, hash string, err error) {
	raw := make([]byte, sshTicketBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	ticket = base64.RawURLEncoding.EncodeToString(raw)
	return ticket, sshTicketHash(ticket), nil
}

// sshTicketHash is how a ticket is stored and looked up: its SHA-256, hex.
func sshTicketHash(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

// sshNamePattern is a container or tty name.
var sshNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// validSSHName checks a container or tty name. "." and "..", which the
// pattern admits, name no container and are refused: the name ends up in
// paths on the device.
func validSSHName(name string) bool {
	return len(name) <= maxSSHNameLength && sshNamePattern.MatchString(name) && name != "." && name != ".."
}

// ValidateSSHTarget checks a target, the SSH user name the browser logs in
// with: "_pv_" (the Pantavisor host), a container name, or
// "<tty>@<container>".
func ValidateSSHTarget(target string) error {
	if target == SSHTargetHost {
		return nil
	}
	if target == "" {
		return errors.New("target must not be empty")
	}
	tty, container, console := strings.Cut(target, "@")
	if !console {
		if !validSSHName(target) {
			return errors.New("target must be _pv_, a container name ([A-Za-z0-9._-], at most 64) or <tty>@<container>")
		}
		return nil
	}
	if !validSSHName(tty) || !validSSHName(container) {
		return errors.New("target <tty>@<container> needs a tty and a container name ([A-Za-z0-9._-], at most 64 each)")
	}
	return nil
}

// ValidateSSHPubkey checks the browser's public key, an OpenSSH
// authorized_keys line of type ssh-ed25519, and returns it normalised to
// "ssh-ed25519 <base64>": a comment is dropped, since the device writes the
// key into authorized_keys with a comment of its own.
func ValidateSSHPubkey(pubkey string) (string, error) {
	if len(pubkey) > maxSSHKeyLength {
		return "", errors.New("pubkey is longer than " + strconv.Itoa(maxSSHKeyLength) + " bytes")
	}
	if strings.ContainsFunc(pubkey, func(r rune) bool { return unicode.IsControl(r) || r == unicode.ReplacementChar }) {
		return "", errors.New("pubkey contains a character that is not allowed")
	}
	fields := strings.Fields(pubkey)
	if len(fields) < 2 {
		return "", errors.New("pubkey must be an ssh-ed25519 public key (\"ssh-ed25519 AAAA...\")")
	}
	if fields[0] != ssh.KeyAlgoED25519 {
		return "", errors.New("pubkey must be of type ssh-ed25519")
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return "", errors.New("pubkey is not valid base64")
	}
	key, err := ssh.ParsePublicKey(blob)
	if err != nil || key.Type() != ssh.KeyAlgoED25519 {
		return "", errors.New("pubkey is not a valid ssh-ed25519 public key")
	}
	return ssh.KeyAlgoED25519 + " " + base64.StdEncoding.EncodeToString(key.Marshal()), nil
}

// ValidateSSHSessionRequest checks a request and returns it normalised.
func ValidateSSHSessionRequest(req SSHSessionRequest) (SSHSessionRequest, error) {
	if err := ValidateSSHTarget(req.Target); err != nil {
		return req, err
	}
	pubkey, err := ValidateSSHPubkey(req.Pubkey)
	if err != nil {
		return req, err
	}
	req.Pubkey = pubkey
	return req, nil
}

var (
	errSSHDeviceLimit = errors.New("this device already has " + strconv.Itoa(MaxSSHSessionsPerDevice) +
		" live SSH sessions, stop one or try again when one ends")
	errSSHUserLimit = errors.New("you already have " + strconv.Itoa(MaxSSHSessionsPerUser) +
		" live SSH sessions, stop one or try again when one ends")
	errSSHStartRateLimited      = errors.New("too many SSH sessions started for this device, try again in a minute")
	errSSHOwnerStartRateLimited = errors.New("too many SSH sessions started, try again in a minute")
)

// insertSSHSession stores a new live session in a free device slot and a free
// user slot, or refuses with errSSHDeviceLimit / errSSHUserLimit; see
// insertLogSession, whose index names it shares.
func (a *App) insertSSHSession(ctx context.Context, session *SSHSession) error {
	sessions := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection)

	deviceSlot, userSlot := 0, 0
	for deviceSlot < MaxSSHSessionsPerDevice && userSlot < MaxSSHSessionsPerUser {
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
	if deviceSlot >= MaxSSHSessionsPerDevice {
		return errSSHDeviceLimit
	}
	return errSSHUserLimit
}

// endSSHSessionUpdate ends a session for reason.
func endSSHSessionUpdate(now time.Time, by, reason string) bson.M {
	return bson.M{"$set": bson.M{
		"live":     false,
		"ended_at": now,
		"ended_by": by,
		"reason":   reason,
	}}
}

// EndSSHSession ends a live session of a device for reason, reporting whether
// it was this call that ended it, and audits the end. The frames of a session
// it ended are deleted: nobody forwards them any more (the WebSocket, when
// there is one, has every frame stored so far from its change stream, and
// deletes what lands later itself), and the notifier's stop to the device is
// built from the session document, not from a frame. A frame the device sends
// within SSHSessionGrace after this is stored again for the WebSocket and
// otherwise left to the TTL.
func EndSSHSession(ctx context.Context, client *mongo.Client, sessionID, deviceID primitive.ObjectID, by, reason string, now time.Time) (bool, error) {
	db := client.Database(utils.MongoDb)
	res, err := db.Collection(SSHSessionsCollection).UpdateOne(ctx,
		bson.M{"_id": sessionID, "device_id": deviceID, "live": true},
		endSSHSessionUpdate(now, by, reason))
	if err != nil {
		return false, err
	}
	ended := res.ModifiedCount > 0
	if ended {
		if _, err := db.Collection(SSHFramesCollection).DeleteMany(ctx, bson.M{"session": sessionID}); err != nil {
			// The TTL is the backstop; the end itself stands.
			log.Println("devices: cannot delete the frames of ended ssh session " + sessionID.Hex() + ": " + err.Error())
		}
	}
	if err := AuditSSHSessionEnd(ctx, client, bson.M{"_id": sessionID}); err != nil {
		log.Println("devices: cannot audit the end of ssh session " + sessionID.Hex() + ": " + err.Error())
	}
	return ended, nil
}

// endStaleSSHSessionsUpdate ends every live session whose lease has run out,
// or that has been idle too long, with its reason.
func endStaleSSHSessionsUpdate(now time.Time) mongo.Pipeline {
	return mongo.Pipeline{{{Key: "$set", Value: bson.M{
		"live":     false,
		"ended_at": now,
		"ended_by": SSHEndedByHub,
		"reason": bson.M{"$switch": bson.M{
			"branches": bson.A{
				bson.M{"case": bson.M{"$lte": bson.A{"$deadline", now}}, "then": SSHEndDeadline},
				bson.M{"case": bson.M{"$lte": bson.A{"$expires_at", now}}, "then": SSHEndExpired},
			},
			"default": SSHEndIdle,
		}},
	}}}}
}

// EndStaleSSHSessions ends the live sessions matching filter whose lease or
// deadline has passed or that have been idle SSHSessionIdle, as
// EndStaleLogSessions does for log sessions.
func EndStaleSSHSessions(ctx context.Context, client *mongo.Client, filter bson.M, now time.Time) error {
	match := bson.M{"live": true, "$or": bson.A{
		bson.M{"expires_at": bson.M{"$lte": now}},
		bson.M{"active_at": bson.M{"$lte": now.Add(-SSHSessionIdle)}},
	}}
	for key, value := range filter {
		match[key] = value
	}
	_, err := client.Database(utils.MongoDb).Collection(SSHSessionsCollection).
		UpdateMany(ctx, match, endStaleSSHSessionsUpdate(now))
	return err
}

// SSHSessionSweepInterval is how often RunSSHSessionSweep runs.
const SSHSessionSweepInterval = 15 * time.Second

// RunSSHSessionSweep ends stale sessions and audits every ended session not
// audited yet, every SSHSessionSweepInterval until ctx is cancelled. Every
// replica may run it: ending is idempotent, and each end is audited once.
func RunSSHSessionSweep(ctx context.Context, client *mongo.Client) {
	ticker := time.NewTicker(SSHSessionSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := EndStaleSSHSessions(sweepCtx, client, nil, time.Now().UTC()); err != nil && ctx.Err() == nil {
				log.Println("devices: cannot end stale ssh sessions: " + err.Error())
			}
			if err := AuditSSHSessionEnd(sweepCtx, client, nil); err != nil && ctx.Err() == nil {
				log.Println("devices: cannot audit ended ssh sessions: " + err.Error())
			}
			cancel()
		}
	}
}

// maxSSHAuditsPerCall bounds the ends one AuditSSHSessionEnd call records.
const maxSSHAuditsPerCall = 1000

// AuditSSHSessionEnd records the end of every ended session matching filter
// that has not been audited yet: who, which device and target, how long and
// how many bytes each way. Each end is claimed before it is recorded, so it is
// recorded once whichever replica gets there first.
func AuditSSHSessionEnd(ctx context.Context, client *mongo.Client, filter bson.M) error {
	sessions := client.Database(utils.MongoDb).Collection(SSHSessionsCollection)
	match := bson.M{"live": false, "end_audited": false}
	for key, value := range filter {
		match[key] = value
	}
	for i := 0; i < maxSSHAuditsPerCall; i++ {
		session := SSHSession{}
		err := sessions.FindOneAndUpdate(ctx, match, bson.M{"$set": bson.M{"end_audited": true}},
			options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&session)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil
		}
		if err != nil {
			return err
		}
		auditSSHSessionEnd(&session)
	}
	return nil
}

func auditSSHSessionEnd(session *SSHSession) {
	endedAt := time.Now().UTC()
	if session.EndedAt != nil {
		endedAt = *session.EndedAt
	}
	log.Printf("devices: audit: ssh session %s by %s on device %s (owner %s) ended: target %s, reason %s (by %s), "+
		"duration %s, bytes up %d (%d frames), down %d (%d frames)",
		session.ID.Hex(), session.CreatedBy, session.DeviceID.Hex(), session.Owner,
		strconv.Quote(session.Target), strconv.Quote(session.Reason), session.EndedBy,
		endedAt.Sub(session.CreatedAt).Round(time.Second), session.UpBytes, session.UpFrames,
		session.DownBytes, session.DownFrames)
}

// auditSSHSessionStart records who opened a terminal on which device.
func auditSSHSessionStart(session *SSHSession) {
	fingerprint := ""
	if key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(session.Pubkey)); err == nil {
		fingerprint = ssh.FingerprintSHA256(key)
	}
	log.Printf("devices: audit: ssh session %s started by %s on device %s (owner %s): target %s, key %s",
		session.ID.Hex(), session.CreatedBy, session.DeviceID.Hex(), session.Owner,
		strconv.Quote(session.Target), fingerprint)
}

// sshOwnerLimitKey is the SSHSessionLimitsCollection _id of an owner's start
// rate limit.
func sshOwnerLimitKey(owner string) string {
	return "owner:" + owner
}

// handlePostSSHSession opens an SSH session on a device
// @Summary Open an SSH terminal on a device connected over MQTT
// @Description Asks the device to accept pubkey (ssh-ed25519, the browser's key) for the session and to connect to
// @Description its SSH server; the SSH bytes then travel over GET .../ws. target is the SSH user name: _pv_ (the
// @Description Pantavisor host), a container name, or <tty>@<container>. The session lasts 60 seconds unless renewed,
// @Description 15 minutes without traffic and 60 minutes at most. Only the device owner may open one, and only while
// @Description the device is connected over MQTT. Requires the full API scope or devices.ssh. At most 2 live sessions
// @Description per device and 3 per user, 10 starts per device and 30 per owner a minute.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param body body SSHSessionRequest true "Session"
// @Success 201 {object} SSHSessionView
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 409 {object} utils.RError
// @Failure 413 {object} utils.RError
// @Failure 429 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/ssh-sessions [post]
func (a *App) handlePostSSHSession(c *echo.Context) error {
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

	req := SSHSessionRequest{}
	c.Request().Body = http.MaxBytesReader(nil, c.Request().Body, maxSSHSessionRequestSize)
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return userError(c, "SSH session request is larger than "+strconv.Itoa(maxSSHSessionRequestSize)+" bytes", http.StatusRequestEntityTooLarge)
		}
		return userError(c, "Error parsing SSH session request: "+err.Error(), http.StatusBadRequest)
	}
	req, err = ValidateSSHSessionRequest(req)
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

	limits := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionLimitsCollection)
	reserved, err := reserveWindowSlot(ctx, limits, device.ID, sshStartRateLimit, sshStartRateWindow, time.Now().UTC(), nil, nil)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking the SSH session rate: "+err.Error(), http.StatusInternalServerError)
	}
	if !reserved {
		return userError(c, errSSHStartRateLimited.Error(), http.StatusTooManyRequests)
	}
	reserved, err = reserveWindowSlot(ctx, limits, sshOwnerLimitKey(device.Owner), sshOwnerStartRateLimit, sshStartRateWindow, time.Now().UTC(), nil, nil)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking the SSH session rate: "+err.Error(), http.StatusInternalServerError)
	}
	if !reserved {
		return userError(c, errSSHOwnerStartRateLimited.Error(), http.StatusTooManyRequests)
	}

	// Sessions whose tab was closed free their slots here, before counting.
	err = EndStaleSSHSessions(ctx, a.mongoClient, bson.M{"$and": bson.A{bson.M{"$or": bson.A{
		bson.M{"device_id": device.ID},
		bson.M{"owner": device.Owner},
	}}}}, now)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error ending stale SSH sessions: "+err.Error(), http.StatusInternalServerError)
	}

	session := SSHSession{
		ID:           primitive.NewObjectID(),
		DeviceID:     device.ID,
		Owner:        device.Owner,
		CreatedBy:    caller,
		Target:       req.Target,
		Pubkey:       req.Pubkey,
		CreatedAt:    now,
		ExpiresAt:    now.Add(SSHSessionLease),
		Deadline:     now.Add(SSHSessionMaxDuration),
		Live:         true,
		ActiveAt:     now,
		DownWindowAt: now,
	}

	// The insert is the start: the MQTT notifier on every replica watches
	// this collection and publishes the start to the device.
	if err := a.insertSSHSession(ctx, &session); err != nil {
		if errors.Is(err, errSSHDeviceLimit) || errors.Is(err, errSSHUserLimit) {
			return userError(c, err.Error(), http.StatusTooManyRequests)
		}
		return echoutil.RestErrorWrapper(c, "Error storing SSH session: "+err.Error(), http.StatusInternalServerError)
	}

	auditSSHSessionStart(&session)

	return echoutil.WriteJSON(c, http.StatusCreated, SSHSessionView{
		ID:        session.ID.Hex(),
		Target:    session.Target,
		ExpiresAt: session.ExpiresAt,
		Deadline:  session.Deadline,
	})
}

// findSSHSession loads a session of a device of the caller, answering the
// errors itself; session is nil when it did.
func (a *App) findSSHSession(c *echo.Context) (*SSHSession, error) {
	caller, ok := callerPrn(c)
	if !ok {
		return nil, echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	sessionID, err := primitive.ObjectIDFromHex(c.Param("sid"))
	if err != nil {
		return nil, userError(c, "Invalid SSH session id", http.StatusBadRequest)
	}

	device, err := a.findOwnedDevice(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return nil, echoutil.RestErrorWrapper(c, "Error loading device: "+err.Error(), http.StatusInternalServerError)
	}
	if device == nil {
		return nil, userError(c, "Device not found", http.StatusNotFound)
	}

	session, err := a.loadSSHSession(c.Request().Context(), sessionID, device.ID, caller)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, userError(c, "SSH session not found", http.StatusNotFound)
	}
	if err != nil {
		return nil, echoutil.RestErrorWrapper(c, "Error loading SSH session: "+err.Error(), http.StatusInternalServerError)
	}
	return session, nil
}

// loadSSHSession reads a session of a device and owner, ending it first if its
// lease ran out or it went idle.
func (a *App) loadSSHSession(ctx context.Context, sessionID, deviceID primitive.ObjectID, owner string) (*SSHSession, error) {
	ctxC, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": sessionID, "device_id": deviceID, "owner": owner}
	if err := EndStaleSSHSessions(ctxC, a.mongoClient, filter, time.Now().UTC()); err != nil {
		return nil, err
	}
	session := SSHSession{}
	err := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).FindOne(ctxC, filter).Decode(&session)
	if err != nil {
		return nil, err
	}
	if !session.Live && !session.EndAudited {
		if err := AuditSSHSessionEnd(ctxC, a.mongoClient, bson.M{"_id": session.ID}); err != nil {
			log.Println("devices: cannot audit the end of ssh session " + session.ID.Hex() + ": " + err.Error())
		}
	}
	return &session, nil
}

// handleRenewSSHSession renews the lease of a live SSH session
// @Summary Renew the lease of a live SSH session
// @Description Extends the session by 60 seconds from now, never past its 60 minute deadline.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "SSH session ID"
// @Success 200 {object} SSHSessionRenewal
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 410 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/ssh-sessions/{sid}/renew [post]
func (a *App) handleRenewSSHSession(c *echo.Context) error {
	session, err := a.findSSHSession(c)
	if session == nil {
		return err
	}
	if !session.Live {
		return userError(c, "SSH session has ended ("+session.Reason+")", http.StatusGone)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(SSHSessionLease)
	if expiresAt.After(session.Deadline) {
		expiresAt = session.Deadline
	}

	// Only a session still live, and whose lease has not run out meanwhile,
	// is renewed; the notifier sees the new expires_at and tells the device.
	result, err := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).UpdateOne(ctx,
		bson.M{"_id": session.ID, "live": true, "expires_at": bson.M{"$gt": time.Now().UTC()}},
		bson.M{"$set": bson.M{"expires_at": expiresAt}})
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error renewing SSH session: "+err.Error(), http.StatusInternalServerError)
	}
	if result.MatchedCount == 0 {
		return userError(c, "SSH session has ended", http.StatusGone)
	}

	return echoutil.WriteJSON(c, http.StatusOK, SSHSessionRenewal{
		ID:        session.ID.Hex(),
		ExpiresAt: expiresAt,
		Deadline:  session.Deadline,
	})
}

// handleSSHSessionTicket issues a ticket for the WebSocket of a live SSH session
// @Summary Issue a ticket for the WebSocket of an SSH session
// @Description The session's JWT never goes into the WebSocket handshake, where the chosen subprotocol is echoed back
// @Description to proxies and HAR files: the client asks for a ticket here and offers it as the subprotocol
// @Description "ticket.<ticket>" of GET .../ws. A ticket is bound to its session, lives 30 seconds and is single use;
// @Description a new ticket replaces the previous one. Only the session's SHA-256 and expiry are stored.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "SSH session ID"
// @Success 201 {object} SSHSessionTicket
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 409 {object} utils.RError "a WebSocket is attached"
// @Failure 410 {object} utils.RError "the session has ended"
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/ssh-sessions/{sid}/ticket [post]
func (a *App) handleSSHSessionTicket(c *echo.Context) error {
	session, err := a.findSSHSession(c)
	if session == nil {
		return err
	}
	if !session.Live {
		return userError(c, "SSH session has ended ("+session.Reason+")", http.StatusGone)
	}
	if session.Attached {
		return userError(c, "SSH session already has a WebSocket", http.StatusConflict)
	}

	ticket, hash, err := newSSHTicket()
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error drawing SSH ticket: "+err.Error(), http.StatusInternalServerError)
	}
	// Whole seconds, as expires_at and deadline: the stored time reads back
	// as returned.
	expiresAt := time.Now().UTC().Truncate(time.Second).Add(SSHTicketLifetime)

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	// One ticket at a time: the new one replaces whatever was there. Only a
	// session still live and without a WebSocket takes one.
	result, err := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).UpdateOne(ctx,
		bson.M{"_id": session.ID, "live": true, "attached": false},
		bson.M{"$set": bson.M{"ticket_hash": hash, "ticket_expires_at": expiresAt}})
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error storing SSH ticket: "+err.Error(), http.StatusInternalServerError)
	}
	if result.MatchedCount == 0 {
		return userError(c, "SSH session already has a WebSocket, or has ended", http.StatusConflict)
	}

	log.Printf("devices: audit: ssh session %s: WebSocket ticket issued to %s for device %s (owner %s), valid until %s",
		session.ID.Hex(), session.CreatedBy, session.DeviceID.Hex(), session.Owner, expiresAt.Format(time.RFC3339))

	return echoutil.WriteJSON(c, http.StatusCreated, SSHSessionTicket{
		Ticket:    ticket,
		ExpiresAt: expiresAt,
	})
}

// HandleSSHSessionTicket serves POST /devices/{id}/ssh-sessions/{sid}/ticket
// behind whatever authenticated the caller (KeyJWTPayload); the MQTT
// package's end-to-end tests obtain their WebSocket ticket through it.
func (a *App) HandleSSHSessionTicket(c *echo.Context) error {
	return a.handleSSHSessionTicket(c)
}

// handleDeleteSSHSession stops an SSH session
// @Summary Stop an SSH session
// @Description The device is told to stop and its WebSocket is closed. Stopping a session that has already ended does nothing.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "SSH session ID"
// @Success 204
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id}/ssh-sessions/{sid} [delete]
func (a *App) handleDeleteSSHSession(c *echo.Context) error {
	session, err := a.findSSHSession(c)
	if session == nil {
		return err
	}

	if session.Live {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()
		// The notifier sees the end and tells the device to stop; the
		// WebSocket sees it and closes.
		if _, err := EndSSHSession(ctx, a.mongoClient, session.ID, session.DeviceID, SSHEndedByHub, SSHEndStopped, time.Now().UTC()); err != nil {
			return echoutil.RestErrorWrapper(c, "Error stopping SSH session: "+err.Error(), http.StatusInternalServerError)
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// EndDeviceSSHSessions winds down the SSH sessions of a device that is
// being deleted: each live one is ended (by the Hub, "stopped"), which audits
// its end, and the notifier, seeing the end, tells the device to stop and the
// WebSocket closes; every end not audited yet is audited; the frames and the
// start rate limits go. The session documents stay until SSHSessionRetention:
// they are the record of who opened a terminal where, and the notifier reads
// the ended document back to send the stop.
func (a *App) EndDeviceSSHSessions(ctx context.Context, deviceID primitive.ObjectID) error {
	db := a.mongoClient.Database(utils.MongoDb)
	now := time.Now().UTC()
	cursor, err := db.Collection(SSHSessionsCollection).Find(ctx,
		bson.M{"device_id": deviceID, "live": true}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return err
	}
	live := []struct {
		ID primitive.ObjectID `bson:"_id"`
	}{}
	if err := cursor.All(ctx, &live); err != nil {
		return err
	}
	for _, session := range live {
		if _, err := EndSSHSession(ctx, a.mongoClient, session.ID, deviceID, SSHEndedByHub, SSHEndStopped, now); err != nil {
			return err
		}
	}
	if err := AuditSSHSessionEnd(ctx, a.mongoClient, bson.M{"device_id": deviceID}); err != nil {
		return err
	}
	if _, err := db.Collection(SSHFramesCollection).DeleteMany(ctx, bson.M{"device_id": deviceID}); err != nil {
		return err
	}
	_, err = db.Collection(SSHSessionLimitsCollection).DeleteOne(ctx, bson.M{"_id": deviceID})
	return err
}

// EnsureSSHSessionIndices creates the indexes of the SSH sessions and frames:
// the unique slots that cap live sessions per device and per user, the ones
// the stale-session sweep and the end audit read, the retention TTLs, and the
// unique (session, dir, seq) index frames are read by (a frame sent twice is
// stored once).
func (a *App) EnsureSSHSessionIndices() error {
	ctx, cancel := context.WithTimeout(context.Background(), CreateIndexTimeout)
	defer cancel()

	live := bson.M{"live": true}
	sessions := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection)
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
		{
			Keys:    bson.D{{Key: "active_at", Value: int32(1)}},
			Options: options.Index().SetName("live_active_at").SetPartialFilterExpression(live),
		},
		{
			Keys: bson.D{{Key: "ended_at", Value: int32(1)}},
			Options: options.Index().SetName("unaudited_ends").
				SetPartialFilterExpression(bson.M{"live": false, "end_audited": false}),
		},
		{Keys: bson.D{{Key: "device_id", Value: int32(1)}}},
		{
			Keys:    bson.D{{Key: "created_at", Value: int32(1)}},
			Options: options.Index().SetExpireAfterSeconds(int32(SSHSessionRetention.Seconds())),
		},
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	if err != nil {
		return err
	}

	limits := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionLimitsCollection)
	_, err = limits.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "updated_at", Value: int32(1)}},
		Options: options.Index().SetExpireAfterSeconds(int32(sshLimitsIdle.Seconds())),
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	if err != nil {
		return err
	}

	frames := a.mongoClient.Database(utils.MongoDb).Collection(SSHFramesCollection)
	_, err = frames.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "session", Value: int32(1)}, {Key: "dir", Value: int32(1)}, {Key: "seq", Value: int32(1)}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "device_id", Value: int32(1)}}},
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	if err != nil {
		return err
	}
	return ensureTTLIndex(ctx, frames, "created_at", SSHFrameRetention)
}

// mongoIndexOptionsConflict is the error code of an index created again with
// other options.
const mongoIndexOptionsConflict = 85

// ensureTTLIndex creates the TTL index on field of coll, or changes the
// expiry of the one that exists with another (a retention changed since it
// was created).
func ensureTTLIndex(ctx context.Context, coll *mongo.Collection, field string, ttl time.Duration) error {
	keys := bson.D{{Key: field, Value: int32(1)}}
	seconds := int32(ttl.Seconds())
	_, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    keys,
		Options: options.Index().SetExpireAfterSeconds(seconds),
	}, options.CreateIndexes().SetMaxTime(CreateIndexTimeout))
	var cmdErr mongo.CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Code != mongoIndexOptionsConflict {
		return err
	}
	// The index exists with another expiry. collMod needs a privilege the
	// service's database user may not have; the old expiry is only a
	// backstop (frames are deleted once forwarded, and when their session
	// ends), so failing to change it must not stop the service. An operator
	// with the privilege runs the same command by hand, once.
	if err := coll.Database().RunCommand(ctx, bson.D{
		{Key: "collMod", Value: coll.Name()},
		{Key: "index", Value: bson.D{{Key: "keyPattern", Value: keys}, {Key: "expireAfterSeconds", Value: seconds}}},
	}).Err(); err != nil {
		log.Printf("WARNING: could not change the expiry of %s.%s to %ds, keeping the existing one: %v; "+
			"run this once as a user with collMod on database %s: %s",
			coll.Name(), field, seconds, err, coll.Database().Name(), ttlIndexCollMod(coll.Name(), field, seconds))
	}
	return nil
}

// ttlIndexCollMod is the mongosh command that sets the expiry of the TTL index
// on field of collection to seconds: what ensureTTLIndex runs, for an operator
// to run when the service's database user may not.
func ttlIndexCollMod(collection, field string, seconds int32) string {
	return fmt.Sprintf(`db.runCommand({collMod: %q, index: {keyPattern: {%s: 1}, expireAfterSeconds: %d}})`,
		collection, field, seconds)
}
