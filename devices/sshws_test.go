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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const testSSHJWTKey = "ssh-websocket-test-key"

// ticketPattern is a ticket as the contract describes it.
var ticketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type sshWSFixture struct {
	*sshFixture
	server *httptest.Server
}

// newSSHWSFixture serves the ticket and WebSocket routes behind the devices
// service's own authentication chain: the subprotocol ticket on the
// WebSocket, Basic credentials, the JWT and Auth middlewares on everything
// else, and the ticket route's scope filter.
func newSSHWSFixture(t *testing.T) *sshWSFixture {
	t.Helper()
	f := &sshWSFixture{sshFixture: newSSHFixture(t)}

	timing := defaultSSHRelayTiming
	timing.poll, timing.gap = 50*time.Millisecond, 500*time.Millisecond
	f.app.sshRelayTiming = &timing

	jwtConfig := &jwtauth.Config{Realm: "test", SigningAlgorithm: "HS256", Key: []byte(testSSHJWTKey), Timeout: time.Hour}
	const prefix = "/devices"
	s := echoutil.NewServer("test")
	g := s.Mount(prefix,
		echoutil.If(prefix, isSSHWebSocketPath, echoutil.WebSocketTicket()),
		echoutil.BasicAuthToBearer(&utils.BasicAuthToBearerMiddleware{JWT: jwtConfig, Mongo: f.app.mongoClient}),
		echoutil.If(prefix, needsJWT, echoutil.JWT(jwtConfig)),
		echoutil.If(prefix, needsJWT, echoutil.Auth()),
	)
	g.POST("/:id/ssh-sessions/:sid/ticket", echoutil.ScopeFilter(SSHSessionScopes, f.app.handleSSHSessionTicket))
	g.GET("/:id/ssh-sessions/:sid/ws", f.app.handleSSHWebSocket)
	f.server = httptest.NewServer(s.E)
	t.Cleanup(f.server.Close)
	return f
}

func sshToken(t *testing.T, caller string, scopes ...utils.Scope) string {
	t.Helper()
	names := []string{}
	for _, scope := range scopes {
		names = append(names, scope.String())
	}
	token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, jwtgo.MapClaims{
		"prn": caller, "type": "USER", "scopes": strings.Join(names, " "), "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testSSHJWTKey))
	require.NoError(t, err)
	return token
}

func (f *sshWSFixture) url(device, sid string) string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http") + "/devices/" + device + "/ssh-sessions/" + sid + "/ws"
}

// ticket asks for a WebSocket ticket as the browser does, with the JWT in
// the Authorization header, under the device given by id, PRN or nick.
func (f *sshWSFixture) ticket(t *testing.T, device, sid, token string) (int, SSHSessionTicket, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/devices/"+device+"/ssh-sessions/"+sid+"/ticket", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body := bytes.Buffer{}
	_, _ = body.ReadFrom(resp.Body)
	ticket := SSHSessionTicket{}
	if resp.StatusCode == http.StatusCreated {
		require.NoError(t, json.Unmarshal(body.Bytes(), &ticket))
	}
	return resp.StatusCode, ticket, body.String()
}

// mustTicket is a ticket for the session, as its owner.
func (f *sshWSFixture) mustTicket(t *testing.T, device primitive.ObjectID, sid string) string {
	t.Helper()
	code, ticket, body := f.ticket(t, device.Hex(), sid, sshToken(t, testOwnerPrn, utils.Scopes.DeviceSSH))
	require.Equal(t, http.StatusCreated, code, body)
	return ticket.Ticket
}

// dial opens the session's WebSocket as a browser does, with the ticket in
// the subprotocol, and returns the HTTP status of a refused handshake.
func (f *sshWSFixture) dial(t *testing.T, device, sid, ticket string) (*websocket.Conn, int) {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	if ticket != "" {
		dialer.Subprotocols = []string{"ticket." + ticket}
	}
	conn, resp, err := dialer.Dial(f.url(device, sid), nil)
	if err != nil {
		require.ErrorIs(t, err, websocket.ErrBadHandshake)
		require.NotNil(t, resp)
		if resp.StatusCode == http.StatusUnauthorized {
			assert.NotContains(t, strings.ToLower(resp.Header.Get("WWW-Authenticate")), "basic", "a refusal never asks for Basic credentials")
		}
		return nil, resp.StatusCode
	}
	t.Cleanup(func() { conn.Close() })
	assert.Equal(t, "ticket."+ticket, resp.Header.Get("Sec-WebSocket-Protocol"), "the chosen subprotocol is echoed")
	assert.Equal(t, "ticket."+ticket, conn.Subprotocol())
	return conn, resp.StatusCode
}

// connect asks for a ticket and opens the session's WebSocket with it.
func (f *sshWSFixture) connect(t *testing.T, device primitive.ObjectID, sid string) *websocket.Conn {
	t.Helper()
	conn, code := f.dial(t, device.Hex(), sid, f.mustTicket(t, device, sid))
	require.NotNil(t, conn, "status %d", code)
	return conn
}

// readClose reads until the server closes the socket, returning the binary
// messages before it and the close frame's code and reason.
func readClose(t *testing.T, conn *websocket.Conn) ([][]byte, int, string) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	messages := [][]byte{}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			var closeErr *websocket.CloseError
			require.True(t, errors.As(err, &closeErr), "no close frame: %v", err)
			return messages, closeErr.Code, closeErr.Text
		}
		messages = append(messages, data)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("never: " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A ticket is issued to the owner alone, for a live session without a
// WebSocket, one at a time; only its hash and expiry are stored.
func TestSSHSessionTicket(t *testing.T) {
	f := newSSHWSFixture(t)
	view := f.mustCreate(t, f.connected)
	owner := sshToken(t, testOwnerPrn, utils.Scopes.DeviceSSH)

	code, ticket, body := f.ticket(t, f.connected.Hex(), view.ID, owner)
	require.Equal(t, http.StatusCreated, code, body)
	assert.Regexp(t, ticketPattern, ticket.Ticket)
	assert.WithinDuration(t, time.Now().Add(SSHTicketLifetime), ticket.ExpiresAt, 2*time.Second)
	fields := map[string]interface{}{}
	require.NoError(t, json.Unmarshal([]byte(body), &fields))
	assert.ElementsMatch(t, []string{"ticket", "expires_at"}, keys(fields), "exactly the contract's fields")

	stored := f.load(t, view.ID)
	assert.Equal(t, sshTicketHash(ticket.Ticket), stored.TicketHash)
	assert.Len(t, stored.TicketHash, 64)
	require.NotNil(t, stored.TicketExpiresAt)
	assert.True(t, stored.TicketExpiresAt.Equal(ticket.ExpiresAt))
	raw := bson.M{}
	oid, _ := primitive.ObjectIDFromHex(view.ID)
	require.NoError(t, f.app.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).
		FindOne(context.Background(), bson.M{"_id": oid}).Decode(&raw))
	doc, err := json.Marshal(raw)
	require.NoError(t, err)
	assert.NotContains(t, string(doc), ticket.Ticket, "the ticket itself is never stored")

	// A new ticket replaces the previous one, which then opens nothing.
	code, second, body := f.ticket(t, f.connected.Hex(), view.ID, sshToken(t, testOwnerPrn, utils.Scopes.API))
	require.Equal(t, http.StatusCreated, code, body)
	assert.NotEqual(t, ticket.Ticket, second.Ticket)
	assert.Equal(t, sshTicketHash(second.Ticket), f.load(t, view.ID).TicketHash)
	_, code = f.dial(t, f.connected.Hex(), view.ID, ticket.Ticket)
	assert.Equal(t, http.StatusUnauthorized, code, "the replaced ticket")

	// The same checks as every session route.
	code, _, _ = f.ticket(t, f.connected.Hex(), view.ID, "")
	assert.Equal(t, http.StatusUnauthorized, code, "no token")
	code, _, _ = f.ticket(t, f.connected.Hex(), view.ID, sshToken(t, testOwnerPrn, utils.Scopes.Devices, utils.Scopes.DeviceCommands))
	assert.Equal(t, http.StatusForbidden, code, "device scopes alone")
	code, _, _ = f.ticket(t, f.connected.Hex(), view.ID, sshToken(t, testStrangerPrn, utils.Scopes.API))
	assert.Equal(t, http.StatusNotFound, code, "not the owner")
	code, _, _ = f.ticket(t, f.others[0].Hex(), view.ID, owner)
	assert.Equal(t, http.StatusNotFound, code, "another device")
	code, _, _ = f.ticket(t, f.connected.Hex(), "nothex", owner)
	assert.Equal(t, http.StatusBadRequest, code)
	code, _, body = f.ticket(t, "dev_"+f.connected.Hex(), view.ID, owner)
	assert.Equal(t, http.StatusCreated, code, "by nick: %s", body)

	// None while a WebSocket is attached, none once the session has ended.
	conn := f.connect(t, f.connected, view.ID)
	require.NotNil(t, conn)
	code, _, body = f.ticket(t, f.connected.Hex(), view.ID, owner)
	assert.Equal(t, http.StatusConflict, code, body)
	assert.Empty(t, f.load(t, view.ID).TicketHash, "consumed")
	assert.Nil(t, f.load(t, view.ID).TicketExpiresAt)

	ended := f.mustCreate(t, f.connected)
	rec := f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, ended.ID, "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	code, _, body = f.ticket(t, f.connected.Hex(), ended.ID, owner)
	assert.Equal(t, http.StatusGone, code, body)
	f.set(t, view.ID, bson.M{"expires_at": time.Now().UTC().Add(-time.Second)})
	code, _, body = f.ticket(t, f.connected.Hex(), view.ID, owner)
	assert.Equal(t, http.StatusGone, code, "lease ran out: %s", body)
}

// Only a ticket opens the WebSocket, once, within its lifetime, for its
// session under its device; nothing else authenticates.
func TestSSHWebSocketAuth(t *testing.T) {
	f := newSSHWSFixture(t)
	view := f.mustCreate(t, f.connected)
	owner := sshToken(t, testOwnerPrn, utils.Scopes.DeviceSSH)
	device := f.connected.Hex()

	_, code := f.dial(t, device, view.ID, "")
	assert.Equal(t, http.StatusUnauthorized, code, "no ticket")
	_, code = f.dial(t, device, view.ID, strings.Repeat("x", 43))
	assert.Equal(t, http.StatusUnauthorized, code, "a ticket never issued")
	assert.False(t, f.load(t, view.ID).Attached)

	other := f.mustCreate(t, f.others[0])
	_, code = f.dial(t, device, view.ID, f.mustTicket(t, f.others[0], other.ID))
	assert.Equal(t, http.StatusUnauthorized, code, "another session's ticket")
	assert.False(t, f.load(t, view.ID).Attached)
	assert.False(t, f.load(t, other.ID).Attached)
	assert.NotEmpty(t, f.load(t, other.ID).TicketHash, "the other session's ticket is not spent")

	ticket := f.mustTicket(t, f.connected, view.ID)
	_, code = f.dial(t, f.others[0].Hex(), view.ID, ticket)
	assert.Equal(t, http.StatusUnauthorized, code, "the session under another device")
	_, code = f.dial(t, primitive.NewObjectID().Hex(), view.ID, ticket)
	assert.Equal(t, http.StatusUnauthorized, code, "the session under an unknown device")
	_, code = f.dial(t, device, primitive.NewObjectID().Hex(), ticket)
	assert.Equal(t, http.StatusUnauthorized, code, "an unknown session")
	// Not a session id: not the WebSocket route either, so the JWT chain
	// answers.
	_, code = f.dial(t, device, "nothex", ticket)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.NotEmpty(t, f.load(t, view.ID).TicketHash, "a wrong path spends no ticket")

	f.set(t, view.ID, bson.M{"ticket_expires_at": time.Now().UTC().Add(-time.Second)})
	_, code = f.dial(t, device, view.ID, ticket)
	assert.Equal(t, http.StatusUnauthorized, code, "expired")
	assert.False(t, f.load(t, view.ID).Attached)

	// Only the subprotocol ticket authenticates: a bearer token offered
	// there would be echoed back in the 101 (and recorded by whatever logs
	// response headers); an Authorization header, which a browser may
	// attach on its own (Basic with a cached personal token), is refused,
	// even a valid one, without asking for Basic credentials; and nothing
	// is attached.
	prev := utils.BasicAuthTokenFactory
	t.Cleanup(func() { utils.BasicAuthTokenFactory = prev })
	utils.BasicAuthTokenFactory = func(context.Context, string, string, *jwtauth.Config, *mongo.Client, time.Duration) (string, *utils.RError) {
		return sshToken(t, testOwnerPrn, utils.Scopes.API), nil
	}
	ticket = f.mustTicket(t, f.connected, view.ID)
	for name, header := range map[string]http.Header{
		"bearer offer":                   {"Sec-WebSocket-Protocol": {"bearer." + owner}},
		"bearer offer + ticket":          {"Sec-WebSocket-Protocol": {"bearer." + owner + ", ticket." + ticket}},
		"bearer header":                  {"Authorization": {"Bearer " + owner}},
		"bearer header + ticket":         {"Authorization": {"Bearer " + owner}, "Sec-WebSocket-Protocol": {"ticket." + ticket}},
		"basic personal token":           {"Authorization": {"Basic dXNlcjpwYXQ="}},
		"basic personal token + ticket":  {"Authorization": {"Basic dXNlcjpwYXQ="}, "Sec-WebSocket-Protocol": {"ticket." + ticket}},
		"cookie":                         {"Cookie": {"session=" + owner}},
		"ticket in the header, no offer": {"Authorization": {"Bearer " + ticket}},
	} {
		dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
		_, resp, err := dialer.Dial(f.url(device, view.ID), header)
		require.ErrorIs(t, err, websocket.ErrBadHandshake, name)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
		assert.NotContains(t, strings.ToLower(resp.Header.Get("WWW-Authenticate")), "basic", name)
		assert.False(t, f.load(t, view.ID).Attached, name)
		assert.Equal(t, sshTicketHash(ticket), f.load(t, view.ID).TicketHash, "%s: the ticket beside a refused credential is not spent", name)
	}
	for name, header := range map[string]http.Header{
		"with the header": {"Authorization": {"Bearer " + owner}},
		"with the offer":  {"Sec-WebSocket-Protocol": {"ticket." + ticket}},
		"with nothing":    {},
	} {
		req, err := http.NewRequest(http.MethodGet, f.server.URL+"/devices/"+device+"/ssh-sessions/"+view.ID+"/ws", nil)
		require.NoError(t, err)
		for k, v := range header {
			req.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "not an upgrade, "+name)
	}
	assert.False(t, f.load(t, view.ID).Attached)

	// The ticket opens the session once: the upgrade consumes it.
	conn, code := f.dial(t, device, view.ID, ticket)
	require.NotNil(t, conn)
	assert.Equal(t, http.StatusSwitchingProtocols, code)
	stored := f.load(t, view.ID)
	assert.True(t, stored.Attached)
	assert.Empty(t, stored.TicketHash)
	assert.Nil(t, stored.TicketExpiresAt)
	_, code = f.dial(t, device, view.ID, ticket)
	assert.Equal(t, http.StatusUnauthorized, code, "the same ticket again")
	_, code = f.dial(t, device, view.ID, strings.Repeat("y", 43))
	assert.Equal(t, http.StatusUnauthorized, code, "one WebSocket per session, and no ticket to tell")

	// An ended session: its ticket no longer opens it.
	ended := f.mustCreate(t, f.connected)
	ticket = f.mustTicket(t, f.connected, ended.ID)
	f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, ended.ID, "")
	_, code = f.dial(t, device, ended.ID, ticket)
	assert.Equal(t, http.StatusUnauthorized, code)
	// A lease that ran out is ended on the way, not attached to.
	lapsed := f.mustCreate(t, f.connected)
	ticket = f.mustTicket(t, f.connected, lapsed.ID)
	f.set(t, lapsed.ID, bson.M{"expires_at": time.Now().UTC().Add(-time.Second)})
	_, code = f.dial(t, device, lapsed.ID, ticket)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.False(t, f.load(t, lapsed.ID).Live)
	assert.Equal(t, SSHEndExpired, f.load(t, lapsed.ID).Reason)

	// A cookie alongside the ticket is ignored, not refused; the device may
	// be named by nick.
	ticket = f.mustTicket(t, f.others[0], other.ID)
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, Subprotocols: []string{"ticket." + ticket}}
	withCookie, resp, err := dialer.Dial(f.url("dev_"+f.others[0].Hex(), other.ID), http.Header{"Cookie": {"session=x"}})
	require.NoError(t, err)
	withCookie.Close()
	assert.Equal(t, "ticket."+ticket, resp.Header.Get("Sec-WebSocket-Protocol"))
}

func TestSSHWebSocketRelay(t *testing.T) {
	f := newSSHWSFixture(t)
	view := f.mustCreate(t, f.connected)
	conn := f.connect(t, f.connected, view.ID)

	// The browser speaks first; nothing reaches the device before its ready
	// frame.
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("SSH-2.0-browser\r\n")))
	time.Sleep(300 * time.Millisecond)
	assert.Empty(t, f.frames(t, view.ID, SSHDirUp), "up bytes before the device is ready")

	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, "", false, "")))
	eventually(t, "the first up frame", func() bool { return len(f.frames(t, view.ID, SSHDirUp)) == 1 })
	up := f.frames(t, view.ID, SSHDirUp)[0]
	assert.EqualValues(t, 0, up.Seq)
	assert.Equal(t, []byte("SSH-2.0-browser\r\n"), up.Data)
	assert.Equal(t, f.connected, up.DeviceID)

	// Out of order on the way down, in order on the socket.
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 2, "world", false, "")))
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 1, "hello ", false, "")))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	for _, want := range []string{"hello ", "world"} {
		kind, data, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, websocket.BinaryMessage, kind)
		assert.Equal(t, want, string(data))
	}
	// Written, so deleted from the store, and so is a redelivered copy.
	eventually(t, "the delivered down frames deleted", func() bool { return len(f.frames(t, view.ID, SSHDirDown)) == 0 })
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 1, "hello ", false, "")))
	eventually(t, "the redelivered frame deleted", func() bool { return len(f.frames(t, view.ID, SSHDirDown)) == 0 })

	// A large message is split into frames of at most 32 KiB.
	big := bytes.Repeat([]byte("p"), MaxSSHFrameData+8*1024)
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, big))
	eventually(t, "the split frames", func() bool { return len(f.frames(t, view.ID, SSHDirUp)) == 3 })
	sizes := map[int64]int{}
	for _, frame := range f.frames(t, view.ID, SSHDirUp) {
		sizes[frame.Seq] = len(frame.Data)
	}
	assert.Equal(t, map[int64]int{0: len("SSH-2.0-browser\r\n"), 1: MaxSSHFrameData, 2: 8 * 1024}, sizes)

	// Closing the socket stops the session, and its end is audited.
	require.NoError(t, conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")))
	eventually(t, "the session stopped", func() bool { return !f.load(t, view.ID).Live })
	stored := f.load(t, view.ID)
	assert.Equal(t, SSHEndStopped, stored.Reason)
	assert.Equal(t, SSHEndedByHub, stored.EndedBy)
	assert.True(t, stored.EndAudited)
	assert.EqualValues(t, len("SSH-2.0-browser\r\n")+len(big), stored.UpBytes)
	assert.EqualValues(t, 3, stored.UpFrames)
	assert.EqualValues(t, len("hello world")+len("hello "), stored.DownBytes)
	eventually(t, "every frame of the session deleted", func() bool {
		return len(f.frames(t, view.ID, SSHDirUp)) == 0 && len(f.frames(t, view.ID, SSHDirDown)) == 0
	})
}

// The end of a session closes its socket with the reason.
func TestSSHWebSocketSessionEnds(t *testing.T) {
	f := newSSHWSFixture(t)

	t.Run("stopped", func(t *testing.T) {
		view := f.mustCreate(t, f.connected)
		conn := f.connect(t, f.connected, view.ID)
		time.Sleep(100 * time.Millisecond)
		rec := f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
		_, code, reason := readClose(t, conn)
		assert.Equal(t, websocket.CloseNormalClosure, code)
		assert.Equal(t, SSHEndStopped, reason)
	})

	t.Run("refused by the device", func(t *testing.T) {
		view := f.mustCreate(t, f.connected)
		conn := f.connect(t, f.connected, view.ID)
		require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, "", true, "busy")))
		_, code, reason := readClose(t, conn)
		assert.Equal(t, websocket.CloseNormalClosure, code)
		assert.Equal(t, "busy", reason)
	})

	t.Run("closed after its last bytes", func(t *testing.T) {
		view := f.mustCreate(t, f.others[0])
		conn := f.connect(t, f.others[0], view.ID)
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 0, "", false, "")))
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 1, "bye", false, "")))
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 2, "", true, "")))
		messages, _, reason := readClose(t, conn)
		assert.Equal(t, [][]byte{[]byte("bye")}, messages)
		assert.Equal(t, SSHEndClosed, reason)
		assert.Equal(t, SSHEndedByDevice, f.load(t, view.ID).EndedBy)
	})

	t.Run("lost frame", func(t *testing.T) {
		view := f.mustCreate(t, f.others[0])
		conn := f.connect(t, f.others[0], view.ID)
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 0, "", false, "")))
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 2, "after the gap", false, "")))
		started := time.Now()
		messages, _, reason := readClose(t, conn)
		assert.Empty(t, messages, "nothing past the gap")
		assert.Equal(t, SSHEndLostFrame, reason)
		assert.GreaterOrEqual(t, time.Since(started), f.app.sshTiming().gap-100*time.Millisecond)
		stored := f.load(t, view.ID)
		assert.Equal(t, SSHEndLostFrame, stored.Reason)
		assert.Equal(t, SSHEndedByHub, stored.EndedBy, "the device is told to stop")
	})

	// The gap is timed from the oldest frame waiting for it: in-order
	// frames trickling in meanwhile do not reset it.
	t.Run("lost frame behind in-order frames", func(t *testing.T) {
		view := f.mustCreate(t, f.others[0])
		conn := f.connect(t, f.others[0], view.ID)
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 0, "", false, "")))
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 100, "held back", false, "")))
		started := time.Now()
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for seq := int64(1); seq < 100; seq++ {
				select {
				case <-stop:
					return
				case <-time.After(f.app.sshTiming().gap / 10):
				}
				_ = f.storeDown(f.others[0], view.ID, SSHFrame{Seq: seq, Data: []byte("x")})
			}
		}()
		messages, _, reason := readClose(t, conn)
		close(stop)
		<-done
		assert.NotEmpty(t, messages, "the in-order frames went through")
		assert.Equal(t, SSHEndLostFrame, reason)
		assert.Less(t, time.Since(started), 3*f.app.sshTiming().gap, "the in-order frames kept the gap open")
	})

	t.Run("overflow by frames", func(t *testing.T) {
		view := f.mustCreate(t, f.others[0])
		conn := f.connect(t, f.others[0], view.ID)
		require.NoError(t, f.storeDown(f.others[0], view.ID, downFrame(t, 0, "", false, "")))
		// Stored at once (as the bridge would, frame by frame, but faster
		// than the gap timeout of the tests).
		sid, err := primitive.ObjectIDFromHex(view.ID)
		require.NoError(t, err)
		held := []interface{}{}
		for seq := int64(2); seq < 2+sshMaxBufferedFrames+1; seq++ {
			held = append(held, SSHFrame{ID: primitive.NewObjectID(), Session: sid, DeviceID: f.others[0], Dir: SSHDirDown,
				Seq: seq, Data: []byte("x"), CreatedAt: time.Now().UTC()})
		}
		_, err = f.app.mongoClient.Database(utils.MongoDb).Collection(SSHFramesCollection).InsertMany(context.Background(), held)
		require.NoError(t, err)
		messages, _, reason := readClose(t, conn)
		assert.Empty(t, messages)
		assert.Equal(t, SSHEndOverflow, reason)
		assert.Equal(t, SSHEndOverflow, f.load(t, view.ID).Reason)
	})

	t.Run("overflow by bytes", func(t *testing.T) {
		view := f.mustCreate(t, f.others[1])
		conn := f.connect(t, f.others[1], view.ID)
		require.NoError(t, f.storeDown(f.others[1], view.ID, downFrame(t, 0, "", false, "")))
		chunk := strings.Repeat("b", MaxSSHFrameData)
		for seq := int64(2); seq < 2+sshMaxBufferedBytes/MaxSSHFrameData+1; seq++ {
			require.NoError(t, f.storeDown(f.others[1], view.ID, downFrame(t, seq, chunk, false, "")))
		}
		_, _, reason := readClose(t, conn)
		assert.Equal(t, SSHEndOverflow, reason)
	})

	t.Run("text message", func(t *testing.T) {
		view := f.mustCreate(t, f.others[1])
		conn := f.connect(t, f.others[1], view.ID)
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("ls")))
		_, code, _ := readClose(t, conn)
		assert.Equal(t, websocket.CloseUnsupportedData, code)
		eventually(t, "the session stopped", func() bool { return !f.load(t, view.ID).Live })
	})

	t.Run("expired", func(t *testing.T) {
		view := f.mustCreate(t, f.others[1])
		conn := f.connect(t, f.others[1], view.ID)
		f.set(t, view.ID, map[string]interface{}{"expires_at": time.Now().UTC().Add(-time.Second)})
		_, _, reason := readClose(t, conn)
		assert.Equal(t, SSHEndExpired, reason)
	})
}

func TestByteLimiter(t *testing.T) {
	l := newByteLimiter(100*1024, 10*1024)
	started := time.Now()
	require.NoError(t, l.wait(context.Background(), 10*1024), "the burst is free")
	assert.Less(t, time.Since(started), 50*time.Millisecond)
	require.NoError(t, l.wait(context.Background(), 20*1024))
	assert.GreaterOrEqual(t, time.Since(started), 150*time.Millisecond, "20 KiB past the burst at 100 KiB/s")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Error(t, l.wait(ctx, 100*1024))
}

func TestIsSSHWebSocketPath(t *testing.T) {
	sid := primitive.NewObjectID().Hex()
	for path, want := range map[string]bool{
		"/dev1/ssh-sessions/" + sid + "/ws":       true,
		"/dev1/ssh-sessions/" + sid + "/renew":    false,
		"/dev1/ssh-sessions/" + sid + "/ticket":   false,
		"/dev1/ssh-sessions/nothex/ws":            false,
		"/dev1/log-sessions/" + sid + "/ws":       false,
		"//ssh-sessions/" + sid + "/ws":           false,
		"/dev1/ssh-sessions/" + sid + "/ws/extra": false,
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		assert.Equal(t, want, isSSHWebSocketPath(r), path)
	}
	assert.False(t, isSSHWebSocketPath(httptest.NewRequest(http.MethodPost, "/dev1/ssh-sessions/"+sid+"/ws", nil)))
}

// The JWT middleware runs on every route needsAuth covers, except the SSH
// WebSocket, ticket route included.
func TestNeedsJWT(t *testing.T) {
	sid := primitive.NewObjectID().Hex()
	for _, tc := range []struct {
		method, path string
		bearer, want bool
	}{
		{http.MethodGet, "/dev1/ssh-sessions/" + sid + "/ws", false, false},
		{http.MethodGet, "/dev1/ssh-sessions/" + sid + "/ws", true, false},
		{http.MethodPost, "/dev1/ssh-sessions/" + sid + "/ticket", false, true},
		{http.MethodPost, "/dev1/ssh-sessions/" + sid + "/renew", false, true},
		{http.MethodGet, "/dev1", false, true},
		{http.MethodPost, "/", false, false},
		{http.MethodPost, "/", true, true},
		{http.MethodPost, "/register", false, false},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.bearer {
			r.Header.Set("Authorization", "Bearer x")
		}
		assert.Equal(t, tc.want, needsJWT(r), "%s %s (bearer %v)", tc.method, tc.path, tc.bearer)
	}
}
