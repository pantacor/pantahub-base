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
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/mongoutils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// The WebSocket of an SSH session carries the raw SSH bytes both ways as
// binary messages. The replica serving it:
//
//   - forwards the session's down frames, which the MQTT bridge of whichever
//     replica holds the device's connection stores: a change stream on the
//     frames (dir "down", this session) delivers them, and the relay writes
//     them in seq order, buffering the ones that arrive early (at most
//     sshMaxBufferedFrames and sshMaxBufferedBytes, or the session ends with
//     "overflow"); a frame still missing SSHGapTimeout after the oldest
//     buffered frame arrived ends the session; the frames it wrote are
//     deleted by seq watermark every poll, and all the session's frames when
//     the relay stops;
//   - stores the browser's messages as up frames (at most MaxSSHFrameData
//     each, seq from 0, at most SSHRate), which the notifier of the replica
//     holding the device's connection publishes; not before the device's
//     ready frame (down seq 0, no data), since the device drops up frames
//     that come before it;
//   - ends the session when the socket closes, and closes the socket, with
//     the reason, when the session ends.

const (
	// sshMaxMessage caps one message from the browser (a paste); it is
	// split into frames.
	sshMaxMessage = 1 << 20

	// sshMaxBufferedFrames and sshMaxBufferedBytes cap the down frames held
	// while one is missing. MQTT keeps a topic in order, so frames only come
	// early after a redelivery; a device that keeps more back is ended.
	sshMaxBufferedFrames = 256
	sshMaxBufferedBytes  = 1 << 20

	sshWriteTimeout = 10 * time.Second

	// sshCloseReasonMax is what a close frame has room for: a control frame
	// carries 125 bytes, 2 of them the code.
	sshCloseReasonMax = 123
)

// sshRelayTiming is how often a relay checks its session and pings the
// browser, how long it waits for the browser, and how long a missing frame
// may be missing.
type sshRelayTiming struct {
	poll, ping, read, gap time.Duration
}

var defaultSSHRelayTiming = sshRelayTiming{
	poll: time.Second,
	ping: 30 * time.Second,
	read: 90 * time.Second,
	gap:  SSHGapTimeout,
}

// sshTiming is the relay timing of the app: the defaults, unless a test set
// its own before serving.
func (a *App) sshTiming() sshRelayTiming {
	if a.sshRelayTiming != nil {
		return *a.sshRelayTiming
	}
	return defaultSSHRelayTiming
}

// sshUpgrader accepts every origin. The route authenticates with the ticket
// in the subprotocol alone (echoutil.WebSocketTicket refuses an Authorization
// header, Basic included, and a bearer offer, and drops cookies), so a
// cross-site page has no ambient credential to ride on: it can only connect
// with a ticket the session's owner asked for seconds ago. The API has no
// configured list of UI origins to check against (the devices mount's CORS
// allows every origin too), and pvr and other non-browser clients send no
// Origin at all.
var sshUpgrader = websocket.Upgrader{
	ReadBufferSize:  16 * 1024,
	WriteBufferSize: 16 * 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// errSSHTicketRefused is the one answer to a ticket that does not open its
// session: used, expired, wrong, or for another session or device, without
// telling which.
var errSSHTicketRefused = errors.New("invalid, used or expired SSH ticket")

// handleSSHWebSocket connects the caller to an SSH session
// @Summary Connect to an SSH session
// @Description A WebSocket carrying the raw SSH bytes both ways as binary messages. The client offers the ticket
// @Description from POST .../ticket as the subprotocol "ticket.<ticket>", which is echoed back; the upgrade consumes
// @Description the ticket. No other credential is accepted on this route: an Authorization header or a
// @Description "bearer.<token>" offer is refused with 401, as is a used, expired or wrong ticket. One WebSocket per
// @Description session; closing it stops the session, and the end of the session closes it with the reason.
// @Tags devices
// @Param id path string true "ID|PRN|NICK"
// @Param sid path string true "SSH session ID"
// @Success 101
// @Failure 400 {object} utils.RError
// @Failure 401 {object} utils.RError
// @Router /devices/{id}/ssh-sessions/{sid}/ws [get]
func (a *App) handleSSHWebSocket(c *echo.Context) error {
	sessionID, err := primitive.ObjectIDFromHex(c.Param("sid"))
	if err != nil {
		return userError(c, "Invalid SSH session id", http.StatusBadRequest)
	}
	// The ticket is the credential of this route: the JWT middleware does
	// not run on it, and echoutil.WebSocketTicket has refused any other.
	ticket, _ := c.Get(echoutil.KeyWebSocketTicket).(string)
	if ticket == "" {
		return echoutil.WebSocketUnauthorized(c, "WebSocket takes a session ticket as its subprotocol (POST .../ticket)")
	}
	r := c.Request()
	if !websocket.IsWebSocketUpgrade(r) {
		return userError(c, "Expected a WebSocket upgrade", http.StatusBadRequest)
	}

	// Consuming the ticket is the attach claim: one socket per session, and
	// one use per ticket, atomic across replicas.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	session, err := a.claimSSHSession(ctx, sessionID, c.Param("id"), ticket)
	cancel()
	if errors.Is(err, errSSHTicketRefused) {
		log.Printf("devices: audit: ssh session %s: WebSocket refused: %s", sessionID.Hex(), err)
		return echoutil.WebSocketUnauthorized(c, err.Error())
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error attaching to SSH session: "+err.Error(), http.StatusInternalServerError)
	}
	// The caller is whoever opened the session, for the access log.
	c.Set(echoutil.KeyJWTPayload, jwtgo.MapClaims{"prn": session.CreatedBy, "type": "USER"})
	log.Printf("devices: audit: ssh session %s attached by %s on device %s (owner %s)",
		session.ID.Hex(), session.CreatedBy, session.DeviceID.Hex(), session.Owner)

	header := http.Header{}
	if protocol, ok := c.Get(echoutil.KeyWebSocketProtocol).(string); ok && protocol != "" {
		header.Set("Sec-WebSocket-Protocol", protocol)
	}
	conn, err := sshUpgrader.Upgrade(c.Response(), r, header)
	if err != nil {
		// The upgrader answered the error; let the client try again, with
		// a new ticket: this one is spent.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sessions := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection)
		if _, err := sessions.UpdateOne(ctx, bson.M{"_id": session.ID}, bson.M{"$set": bson.M{"attached": false}}); err != nil {
			log.Println("devices: cannot release ssh session " + session.ID.Hex() + ": " + err.Error())
		}
		return nil
	}

	relay := &sshRelay{client: a.mongoClient, app: a, conn: conn, session: session, timing: a.sshTiming()}
	relay.run()
	return nil
}

// claimSSHSession attaches to the session a ticket was issued for and returns
// it, consuming the ticket: the one update matches the session (live, no
// WebSocket, this ticket's hash, not expired), sets attached and clears the
// ticket. deviceRef, the device of the path (id, PRN or nick), must be the
// session's; a nick is looked up as the session's owner sees it. Every way
// the ticket does not fit answers errSSHTicketRefused alike.
func (a *App) claimSSHSession(ctx context.Context, sessionID primitive.ObjectID, deviceRef, ticket string) (*SSHSession, error) {
	sessions := a.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection)

	bound := struct {
		DeviceID primitive.ObjectID `bson:"device_id"`
		Owner    string             `bson:"owner"`
	}{}
	err := sessions.FindOne(ctx, bson.M{"_id": sessionID},
		options.FindOne().SetProjection(bson.M{"device_id": 1, "owner": 1})).Decode(&bound)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errSSHTicketRefused
	}
	if err != nil {
		return nil, err
	}
	deviceID, err := a.ResolveDeviceIDOrNick(ctx, bound.Owner, deviceRef)
	if err != nil {
		if mongoutils.IsNotFound(err) {
			return nil, errSSHTicketRefused
		}
		return nil, err
	}
	if *deviceID != bound.DeviceID {
		return nil, errSSHTicketRefused
	}

	// A lease that ran out is ended first, as loadSSHSession does.
	now := time.Now().UTC()
	if err := EndStaleSSHSessions(ctx, a.mongoClient, bson.M{"_id": sessionID}, now); err != nil {
		return nil, err
	}
	session := SSHSession{}
	err = sessions.FindOneAndUpdate(ctx,
		bson.M{
			"_id": sessionID, "device_id": bound.DeviceID, "live": true, "attached": false,
			"ticket_hash": sshTicketHash(ticket), "ticket_expires_at": bson.M{"$gt": now},
		},
		bson.M{
			"$set":   bson.M{"attached": true},
			"$unset": bson.M{"ticket_hash": "", "ticket_expires_at": ""},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&session)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errSSHTicketRefused
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// HandleSSHWebSocket serves GET /devices/{id}/ssh-sessions/{sid}/ws behind
// echoutil.WebSocketTicket, which puts the offered ticket in the context; the
// MQTT package's end-to-end tests drive the relay through it.
func (a *App) HandleSSHWebSocket(c *echo.Context) error {
	return a.handleSSHWebSocket(c)
}

// sshRelay serves one session's WebSocket.
type sshRelay struct {
	client  *mongo.Client
	app     *App
	conn    *websocket.Conn
	session *SSHSession
	timing  sshRelayTiming
}

// sshUpResult is why the browser side of a relay stopped.
type sshUpResult struct {
	// closed is set when the browser closed or dropped the socket.
	closed bool
	// ended is set when the session ended under it.
	ended bool
	// code and reason, when set, close the socket.
	code   int
	reason string
}

func (s *sshRelay) run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer s.conn.Close()
	// The down frames this relay wrote are deleted from the store once a
	// poll, by seq watermark, and every frame of the session when it stops.
	defer s.dropFrames()

	stream, err := s.openDownStream(ctx, nil)
	if err != nil {
		log.Println("devices: ssh session " + s.session.ID.Hex() + ": cannot watch frames: " + err.Error())
		s.end(SSHEndedByHub, "error: relay unavailable")
		s.close(websocket.CloseInternalServerErr, "error: relay unavailable", nil)
		return
	}

	frames := make(chan SSHFrame, 64)
	streamDone := make(chan error, 1)
	go func() { streamDone <- s.pumpDown(ctx, stream, frames) }()
	// The device drops up frames sent before its ready frame (down seq 0):
	// the browser's first bytes wait for it.
	ready := make(chan struct{})
	upDone := make(chan sshUpResult, 1)
	go func() { upDone <- s.pumpUp(ctx, ready) }()

	poll := time.NewTicker(s.timing.poll)
	defer poll.Stop()
	ping := time.NewTicker(s.timing.ping)
	defer ping.Stop()

	next := int64(0)
	buffered := map[int64]bufferedSSHFrame{}
	bufferedBytes := 0
	// gapSince is when the oldest frame still buffered arrived: the missing
	// frame before it has been missing since at least then.
	var gapSince time.Time
	// deletedBelow is the seq watermark the stored frames are deleted below;
	// staleStored is set when a frame below it was stored again (a
	// redelivery) and needs deleting too.
	deletedBelow := int64(0)
	staleStored := false

	for {
		select {
		case frame := <-frames:
			if frame.Seq < next {
				staleStored = true
				continue
			}
			if _, dup := buffered[frame.Seq]; dup {
				continue
			}
			if frame.Seq != next && (len(buffered) >= sshMaxBufferedFrames || bufferedBytes+len(frame.Data) > sshMaxBufferedBytes) {
				s.overflow(upDone)
				return
			}
			buffered[frame.Seq] = bufferedSSHFrame{frame: frame, at: time.Now()}
			bufferedBytes += len(frame.Data)
			progressed := false
			for {
				b, ok := buffered[next]
				if !ok {
					break
				}
				f := b.frame
				delete(buffered, next)
				bufferedBytes -= len(f.Data)
				if next == 0 {
					close(ready)
				}
				next++
				progressed = true
				if len(f.Data) > 0 {
					_ = s.conn.SetWriteDeadline(time.Now().Add(sshWriteTimeout))
					if err := s.conn.WriteMessage(websocket.BinaryMessage, f.Data); err != nil {
						s.end(SSHEndedByHub, SSHEndStopped)
						return
					}
				}
				if f.End {
					reason := f.Reason
					if reason == "" {
						reason = SSHEndClosed
					}
					s.close(websocket.CloseNormalClosure, reason, upDone)
					return
				}
			}
			switch {
			case len(buffered) == 0:
				gapSince = time.Time{}
			case progressed || gapSince.IsZero():
				gapSince = oldestArrival(buffered)
			}

		case <-poll.C:
			if !gapSince.IsZero() && time.Since(gapSince) >= s.timing.gap {
				s.lost(upDone)
				return
			}
			if next > deletedBelow || staleStored {
				if s.deleteDelivered(ctx, next) {
					deletedBelow, staleStored = next, false
				}
			}
			session, err := s.app.loadSSHSession(ctx, s.session.ID, s.session.DeviceID, s.session.Owner)
			if errors.Is(err, mongo.ErrNoDocuments) {
				s.close(websocket.CloseNormalClosure, SSHEndStopped, upDone)
				return
			}
			if err != nil {
				log.Println("devices: ssh session " + s.session.ID.Hex() + ": cannot load session: " + err.Error())
				continue
			}
			if !session.Live {
				s.close(websocket.CloseNormalClosure, session.Reason, upDone)
				return
			}

		case <-ping.C:
			if err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(sshWriteTimeout)); err != nil {
				s.end(SSHEndedByHub, SSHEndStopped)
				return
			}

		case result := <-upDone:
			if result.ended {
				s.close(websocket.CloseNormalClosure, s.reason(ctx), nil)
				return
			}
			// Closing the socket stops the session.
			s.end(SSHEndedByHub, SSHEndStopped)
			if !result.closed {
				s.close(result.code, result.reason, nil)
			}
			return

		case err := <-streamDone:
			log.Println("devices: ssh session " + s.session.ID.Hex() + ": frames stream ended: " + errText(err))
			s.end(SSHEndedByHub, "error: relay lost")
			s.close(websocket.CloseInternalServerErr, "error: relay lost", upDone)
			return
		}
	}
}

func errText(err error) string {
	if err == nil {
		return "closed"
	}
	return err.Error()
}

// reason is why the session ended, as stored.
func (s *sshRelay) reason(ctx context.Context) string {
	session, err := s.app.loadSSHSession(ctx, s.session.ID, s.session.DeviceID, s.session.Owner)
	if err != nil || session.Reason == "" {
		return SSHEndStopped
	}
	return session.Reason
}

// bufferedSSHFrame is a down frame that came early, and when it came.
type bufferedSSHFrame struct {
	frame SSHFrame
	at    time.Time
}

// oldestArrival is when the frame buffered longest arrived.
func oldestArrival(buffered map[int64]bufferedSSHFrame) time.Time {
	var oldest time.Time
	for _, b := range buffered {
		if oldest.IsZero() || b.at.Before(oldest) {
			oldest = b.at
		}
	}
	return oldest
}

// lost ends the session over a frame that never came.
func (s *sshRelay) lost(upDone <-chan sshUpResult) {
	s.end(SSHEndedByHub, SSHEndLostFrame)
	s.close(websocket.CloseNormalClosure, SSHEndLostFrame, upDone)
}

// overflow ends the session over a device holding back more frames than the
// relay buffers.
func (s *sshRelay) overflow(upDone <-chan sshUpResult) {
	s.end(SSHEndedByHub, SSHEndOverflow)
	s.close(websocket.CloseNormalClosure, SSHEndOverflow, upDone)
}

// deleteDelivered deletes the session's stored down frames below seq next,
// the ones written to the WebSocket (and any redelivered copy of them),
// reporting whether it did.
func (s *sshRelay) deleteDelivered(ctx context.Context, next int64) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.client.Database(utils.MongoDb).Collection(SSHFramesCollection).DeleteMany(ctx,
		bson.M{"session": s.session.ID, "dir": SSHDirDown, "seq": bson.M{"$lt": next}})
	if err != nil && ctx.Err() == nil {
		log.Println("devices: ssh session " + s.session.ID.Hex() + ": cannot delete delivered frames: " + err.Error())
	}
	return err == nil
}

// dropFrames deletes every stored frame of the session once the relay stops:
// the session is over, and a notifier publishing a last up frame has it from
// its change event already.
func (s *sshRelay) dropFrames() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.client.Database(utils.MongoDb).Collection(SSHFramesCollection).DeleteMany(ctx,
		bson.M{"session": s.session.ID}); err != nil {
		log.Println("devices: ssh session " + s.session.ID.Hex() + ": cannot delete frames: " + err.Error())
	}
}

// end ends the session, if it is still live; the notifier tells the device.
func (s *sshRelay) end(by, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := EndSSHSession(ctx, s.client, s.session.ID, s.session.DeviceID, by, reason, time.Now().UTC()); err != nil {
		log.Println("devices: cannot end ssh session " + s.session.ID.Hex() + ": " + err.Error())
	}
}

// close sends a close frame with reason and waits a moment for the browser's
// answer (upDone, when the reader still runs) before the socket is closed.
func (s *sshRelay) close(code int, reason string, upDone <-chan sshUpResult) {
	message := websocket.FormatCloseMessage(code, capText(reason, sshCloseReasonMax, ""))
	if err := s.conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second)); err != nil {
		return
	}
	if upDone != nil {
		select {
		case <-upDone:
		case <-time.After(time.Second):
		}
	}
}

// openDownStream watches the down frames of the session, from resumeToken
// when set.
func (s *sshRelay) openDownStream(ctx context.Context, resumeToken bson.Raw) (*mongo.ChangeStream, error) {
	pipeline := mongo.Pipeline{bson.D{{Key: "$match", Value: bson.M{
		"operationType":        "insert",
		"fullDocument.session": s.session.ID,
		"fullDocument.dir":     SSHDirDown,
	}}}}
	opts := options.ChangeStream()
	if len(resumeToken) > 0 {
		opts.SetResumeAfter(resumeToken)
	}
	return s.client.Database(utils.MongoDb).Collection(SSHFramesCollection).Watch(ctx, pipeline, opts)
}

// sshStreamRetries is how often a failed frames stream is resumed.
const sshStreamRetries = 3

// pumpDown sends the session's down frames to frames: those stored before the
// stream opened, then every one the stream delivers. Duplicates are the
// writer's to drop. It returns when ctx is cancelled or the stream cannot be
// resumed.
func (s *sshRelay) pumpDown(ctx context.Context, stream *mongo.ChangeStream, frames chan<- SSHFrame) error {
	send := func(frame SSHFrame) bool {
		select {
		case frames <- frame:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// The stream is capturing already, so nothing falls between the two.
	findCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	cursor, err := s.client.Database(utils.MongoDb).Collection(SSHFramesCollection).Find(findCtx,
		bson.M{"session": s.session.ID, "dir": SSHDirDown},
		options.Find().SetSort(bson.D{{Key: "seq", Value: 1}}).SetLimit(sshMaxBufferedFrames))
	if err == nil {
		for cursor.Next(findCtx) {
			frame := SSHFrame{}
			if cursor.Decode(&frame) == nil && !send(frame) {
				break
			}
		}
		err = cursor.Err()
		_ = cursor.Close(context.Background())
	}
	cancel()
	if err != nil && ctx.Err() == nil {
		closeStream(stream)
		return err
	}

	retries := 0
	for {
		for stream.Next(ctx) {
			event := struct {
				FullDocument SSHFrame `bson:"fullDocument"`
			}{}
			if err := stream.Decode(&event); err != nil {
				log.Println("devices: ssh session " + s.session.ID.Hex() + ": cannot decode frame: " + err.Error())
				continue
			}
			retries = 0
			if !send(event.FullDocument) {
				break
			}
		}
		token := stream.ResumeToken()
		err := stream.Err()
		closeStream(stream)
		if ctx.Err() != nil {
			return nil
		}
		if retries >= sshStreamRetries {
			return err
		}
		retries++
		timer := time.NewTimer(time.Duration(retries) * 500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if stream, err = s.openDownStream(ctx, token); err != nil {
			return err
		}
	}
}

func closeStream(stream *mongo.ChangeStream) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = stream.Close(ctx)
}

// pumpUp stores the browser's binary messages as up frames until the socket
// closes, the browser sends something else, or the session ends. Nothing is
// stored before ready is closed: the device's ready frame has been seen.
func (s *sshRelay) pumpUp(ctx context.Context, ready <-chan struct{}) sshUpResult {
	s.conn.SetReadLimit(sshMaxMessage)
	_ = s.conn.SetReadDeadline(time.Now().Add(s.timing.read))
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(s.timing.read))
	})

	limiter := newByteLimiter(SSHRate, MaxSSHFrameData)
	seq := int64(0)
	for {
		kind, data, err := s.conn.ReadMessage()
		if err != nil {
			return sshUpResult{closed: true}
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(s.timing.read))
		if kind != websocket.BinaryMessage {
			return sshUpResult{code: websocket.CloseUnsupportedData, reason: "binary messages only"}
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return sshUpResult{closed: true}
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(s.timing.read))
		for len(data) > 0 {
			chunk := data
			if len(chunk) > MaxSSHFrameData {
				chunk = chunk[:MaxSSHFrameData]
			}
			data = data[len(chunk):]
			if err := limiter.wait(ctx, len(chunk)); err != nil {
				return sshUpResult{closed: true}
			}
			storeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := storeSSHUpFrame(storeCtx, s.client, s.session, seq, chunk, time.Now().UTC())
			cancel()
			if errors.Is(err, ErrSSHSessionNoLive) {
				// The writer closes the socket with the session's reason.
				return sshUpResult{ended: true}
			}
			if err != nil {
				log.Println("devices: ssh session " + s.session.ID.Hex() + ": cannot store up frame: " + err.Error())
				return sshUpResult{code: websocket.CloseInternalServerErr, reason: "error: relay lost"}
			}
			seq++
		}
	}
}

// byteLimiter paces the up frames at rate bytes a second, with a burst.
type byteLimiter struct {
	rate, burst float64
	tokens      float64
	last        time.Time
}

func newByteLimiter(rate, burst int) *byteLimiter {
	return &byteLimiter{rate: float64(rate), burst: float64(burst), tokens: float64(burst), last: time.Now()}
}

// wait takes n bytes, sleeping until the rate allows them.
func (l *byteLimiter) wait(ctx context.Context, n int) error {
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
	l.tokens -= float64(n)
	if l.tokens >= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(-l.tokens / l.rate * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isSSHWebSocketPath matches GET /<id>/ssh-sessions/<sid>/ws below the
// devices prefix, the one route that takes its credential, a ticket, from the
// subprotocol and not from the JWT middleware.
func isSSHWebSocketPath(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] != "ssh-sessions" || parts[3] != "ws" {
		return false
	}
	_, err := primitive.ObjectIDFromHex(parts[2])
	return err == nil
}
