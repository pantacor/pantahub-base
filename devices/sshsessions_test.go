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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/ssh"
)

// testSSHKey is a fresh ssh-ed25519 public key, as the browser sends it.
func testSSHKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

func TestValidateSSHTarget(t *testing.T) {
	for _, ok := range []string{
		"_pv_", "pvr-sdk", "os", "my.container_1", "A-Z", strings.Repeat("c", 64),
		"console@pvr-sdk", "lxc-console@os", "_pv_@x",
	} {
		assert.NoError(t, ValidateSSHTarget(ok), ok)
	}
	for _, bad := range []string{
		"", ".", "..", "../x", "a/b", "a b", "root;id", "a\nb", "é", strings.Repeat("c", 65),
		"@", "tty@", "@container", "a@b@c", "..@os", "tty@..", "tty@" + strings.Repeat("c", 65),
	} {
		assert.Error(t, ValidateSSHTarget(bad), "%q", bad)
	}
}

func TestValidateSSHPubkey(t *testing.T) {
	key := testSSHKey(t)
	got, err := ValidateSSHPubkey(key)
	require.NoError(t, err)
	assert.Equal(t, key, got)

	got, err = ValidateSSHPubkey("  " + key + " browser@hub  ")
	require.NoError(t, err)
	assert.Equal(t, key, got, "the comment is dropped")

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rsaPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	require.NoError(t, err)
	fields := strings.Fields(key)
	// An rsa blob under an ed25519 label.
	mislabelled := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(rsaPub.Marshal())

	for name, bad := range map[string]string{
		"empty":           "",
		"rsa":             strings.TrimSpace(string(ssh.MarshalAuthorizedKey(rsaPub))),
		"mislabelled":     mislabelled,
		"no key":          "ssh-ed25519",
		"not base64":      "ssh-ed25519 !!!!",
		"truncated":       "ssh-ed25519 " + fields[1][:20],
		"trailing bytes":  "ssh-ed25519 " + base64.StdEncoding.EncodeToString(append(mustDecode(t, fields[1]), 0)),
		"second line":     key + "\nssh-ed25519 " + fields[1],
		"options":         `command="sh" ` + key,
		"control in note": key + " a\x00b",
		"too long":        key + " " + strings.Repeat("x", maxSSHKeyLength),
	} {
		_, err := ValidateSSHPubkey(bad)
		assert.Error(t, err, name)
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestSSHSessionScopes(t *testing.T) {
	scopes := utils.MarshalScopes(SSHSessionScopes)
	for scope, want := range map[string]bool{
		utils.Scopes.API.String():       true,
		utils.Scopes.DeviceSSH.String(): true,
		// Device access, commands or logs do not open a terminal.
		utils.Scopes.Devices.String():        false,
		utils.Scopes.WriteDevices.String():   false,
		utils.Scopes.DeviceCommands.String(): false,
		utils.Scopes.DeviceLogs.String():     false,
		utils.Scopes.APIReadOnly.String():    false,
	} {
		assert.Equal(t, want, utils.MatchScope(scopes, []string{scope}), scope)
	}
	assert.Equal(t, "devices.ssh", utils.Scopes.DeviceSSH.ID)
}

type sshFixture struct {
	*logFixture
	key string
}

func newSSHFixture(t *testing.T) *sshFixture {
	t.Helper()
	f := &sshFixture{logFixture: newLogFixture(t), key: testSSHKey(t)}
	require.NoError(t, f.app.EnsureSSHSessionIndices())
	require.NoError(t, f.app.EnsureSSHSessionIndices(), "idempotent")
	return f
}

func (f *sshFixture) body(target string) string {
	raw, _ := json.Marshal(SSHSessionRequest{Target: target, Pubkey: f.key})
	return string(raw)
}

func (f *sshFixture) create(t *testing.T, caller string, device primitive.ObjectID, body string) (int, SSHSessionView, string) {
	t.Helper()
	rec := testRequest(t, f.app.handlePostSSHSession, caller, http.MethodPost, "/devices/"+device.Hex()+"/ssh-sessions",
		strings.NewReader(body), echo.PathValue{Name: "id", Value: device.Hex()})
	view := SSHSessionView{}
	if rec.Code == http.StatusCreated {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	}
	return rec.Code, view, rec.Body.String()
}

func (f *sshFixture) mustCreate(t *testing.T, device primitive.ObjectID) SSHSessionView {
	t.Helper()
	code, view, body := f.create(t, testOwnerPrn, device, f.body("_pv_"))
	require.Equal(t, http.StatusCreated, code, body)
	return view
}

func (f *sshFixture) call(t *testing.T, handler func(*echo.Context) error, caller, method string, device primitive.ObjectID, sid, suffix string) *httptest.ResponseRecorder {
	t.Helper()
	return testRequest(t, handler, caller, method, "/devices/"+device.Hex()+"/ssh-sessions/"+sid+suffix, nil,
		echo.PathValue{Name: "id", Value: device.Hex()}, echo.PathValue{Name: "sid", Value: sid})
}

func (f *sshFixture) load(t *testing.T, id string) SSHSession {
	t.Helper()
	oid, err := primitive.ObjectIDFromHex(id)
	require.NoError(t, err)
	session := SSHSession{}
	require.NoError(t, f.app.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).
		FindOne(context.Background(), bson.M{"_id": oid}).Decode(&session))
	return session
}

func (f *sshFixture) set(t *testing.T, id string, set bson.M) {
	t.Helper()
	oid, err := primitive.ObjectIDFromHex(id)
	require.NoError(t, err)
	_, err = f.app.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).
		UpdateOne(context.Background(), bson.M{"_id": oid}, bson.M{"$set": set})
	require.NoError(t, err)
}

func TestPostSSHSession(t *testing.T) {
	f := newSSHFixture(t)

	code, view, body := f.create(t, testOwnerPrn, f.connected, f.body("console@pvr-sdk"))
	require.Equal(t, http.StatusCreated, code, body)
	assert.Equal(t, "console@pvr-sdk", view.Target)
	assert.Equal(t, SSHSessionLease, time.Until(view.ExpiresAt).Round(time.Minute))
	assert.Equal(t, SSHSessionMaxDuration-SSHSessionLease, view.Deadline.Sub(view.ExpiresAt))
	fields := map[string]interface{}{}
	require.NoError(t, json.Unmarshal([]byte(body), &fields))
	assert.ElementsMatch(t, []string{"id", "target", "expires_at", "deadline"}, keys(fields), "exactly the contract's fields")

	stored := f.load(t, view.ID)
	assert.True(t, stored.Live)
	assert.Equal(t, testOwnerPrn, stored.Owner)
	assert.Equal(t, testOwnerPrn, stored.CreatedBy)
	assert.Equal(t, f.key, stored.Pubkey)
	assert.Equal(t, f.connected, stored.DeviceID)
	assert.False(t, stored.Attached)
	assert.False(t, stored.EndAudited)
	assert.True(t, stored.ActiveAt.Equal(stored.CreatedAt), "active from the start")

	for body, want := range map[string]string{
		`{"target":"../x","pubkey":"` + f.key + `"}`: "target",
		`{"target":"","pubkey":"` + f.key + `"}`:     "target must not be empty",
		`{"target":"_pv_"}`:                          "pubkey",
		`{"target":"_pv_","pubkey":"ssh-rsa AAAA"}`:  "ssh-ed25519",
		`not json`: "Error parsing",
	} {
		code, _, got := f.create(t, testOwnerPrn, f.connected, body)
		assert.Equal(t, http.StatusBadRequest, code, body)
		assert.Contains(t, got, want, "the reason reaches the caller: %s", body)
	}
	code, _, body = f.create(t, testOwnerPrn, f.connected, `{"target":"`+strings.Repeat("x", maxSSHSessionRequestSize)+`"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code, body)

	code, _, body = f.create(t, testOwnerPrn, f.offline, f.body("_pv_"))
	assert.Equal(t, http.StatusConflict, code, body)
	assert.Contains(t, body, "not connected over MQTT")
	code, _, body = f.create(t, testOwnerPrn, f.orphaned, f.body("_pv_"))
	assert.Equal(t, http.StatusConflict, code, "connected to a replica without heartbeat: %s", body)

	code, _, body = f.create(t, testStrangerPrn, f.connected, f.body("_pv_"))
	assert.Equal(t, http.StatusNotFound, code, body)
	code, _, body = f.create(t, testOwnerPrn, primitive.NewObjectID(), f.body("_pv_"))
	assert.Equal(t, http.StatusNotFound, code, body)
}

func keys(m map[string]interface{}) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// devices.ssh or the full API scope opens a session; nothing else does.
func TestSSHSessionRouteScopes(t *testing.T) {
	f := newSSHFixture(t)
	post := echoutil.ScopeFilter(SSHSessionScopes, f.app.handlePostSSHSession)
	id := echo.PathValue{Name: "id", Value: f.connected.Hex()}

	for _, scopes := range [][]string{
		{utils.Scopes.Devices.String()},
		{utils.Scopes.WriteDevices.String(), utils.Scopes.ReadDevices.String()},
		{utils.Scopes.DeviceCommands.String()},
		{utils.Scopes.DeviceLogs.String()},
		{utils.Scopes.APIReadOnly.String()},
		nil,
	} {
		rec := testScopedRequest(t, post, scopes, testOwnerPrn, http.MethodPost, "/", f.body("_pv_"), id)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%v", scopes)
	}
	for _, scopes := range [][]string{{utils.Scopes.DeviceSSH.String()}, {utils.Scopes.API.String()}} {
		rec := testScopedRequest(t, post, scopes, testOwnerPrn, http.MethodPost, "/", f.body("_pv_"), id)
		assert.Equal(t, http.StatusCreated, rec.Code, "%v: %s", scopes, rec.Body.String())
	}
}

// Requests racing each other cannot open more live sessions than the limits.
func TestSSHSessionLimitsAreAtomic(t *testing.T) {
	f := newSSHFixture(t)

	race := func(n int, device primitive.ObjectID) (created int) {
		codes := make(chan int, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, _, _ := f.create(t, testOwnerPrn, device, f.body("_pv_"))
				codes <- code
			}()
		}
		wg.Wait()
		close(codes)
		for code := range codes {
			switch code {
			case http.StatusCreated:
				created++
			case http.StatusTooManyRequests:
			default:
				t.Errorf("unexpected status %d", code)
			}
		}
		return created
	}

	assert.Equal(t, MaxSSHSessionsPerDevice, race(5, f.connected), "sessions opened at once on one device")
	code, _, body := f.create(t, testOwnerPrn, f.connected, f.body("_pv_"))
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Contains(t, body, "this device already has")

	// 2 on the first device, 1 on the second: the user's 3.
	assert.Equal(t, 1, race(4, f.others[0]), "the user's limit")
	code, _, body = f.create(t, testOwnerPrn, f.others[1], f.body("_pv_"))
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Contains(t, body, "you already have")

	live, err := f.app.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).
		CountDocuments(context.Background(), bson.M{"owner": testOwnerPrn, "live": true})
	require.NoError(t, err)
	assert.EqualValues(t, MaxSSHSessionsPerUser, live)

	// Log sessions keep their own limits.
	code, _, body = f.logFixture.create(t, testOwnerPrn, f.connected, logBody)
	assert.Equal(t, http.StatusCreated, code, body)
}

func TestSSHSessionStartRateLimits(t *testing.T) {
	f := newSSHFixture(t)
	for i := range sshStartRateLimit {
		view := f.mustCreate(t, f.connected)
		rec := f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "start %d", i)
	}
	code, _, body := f.create(t, testOwnerPrn, f.connected, f.body("_pv_"))
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Contains(t, body, errSSHStartRateLimited.Error())

	// The owner's limit, across its other devices.
	devices := f.others
	require.Greater(t, sshStartRateLimit+len(devices)*sshStartRateLimit, sshOwnerStartRateLimit)
	for i := sshStartRateLimit; i < sshOwnerStartRateLimit; i++ {
		device := devices[i%len(devices)]
		view := f.mustCreate(t, device)
		rec := f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, device, view.ID, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
	}
	for _, device := range devices {
		code, _, body := f.create(t, testOwnerPrn, device, f.body("_pv_"))
		assert.Equal(t, http.StatusTooManyRequests, code, body)
		assert.Contains(t, body, errSSHOwnerStartRateLimited.Error())
	}
}

// A session ends, and frees its slot, when its lease or deadline passes or it
// goes idle, with nobody stopping it; each end is audited once.
func TestSSHSessionExpiry(t *testing.T) {
	f := newSSHFixture(t)

	expired := f.mustCreate(t, f.connected)
	deadline := f.mustCreate(t, f.connected)
	past := time.Now().UTC().Add(-time.Second)
	f.set(t, expired.ID, bson.M{"expires_at": past})
	f.set(t, deadline.ID, bson.M{"expires_at": past, "deadline": past})

	idle := f.mustCreate(t, f.others[0])
	f.set(t, idle.ID, bson.M{"active_at": time.Now().UTC().Add(-SSHSessionIdle - time.Second)})

	code, _, body := f.create(t, testOwnerPrn, f.connected, f.body("_pv_"))
	assert.Equal(t, http.StatusCreated, code, "stale sessions free their slots: %s", body)
	assert.Equal(t, SSHEndExpired, f.load(t, expired.ID).Reason)
	assert.Equal(t, SSHEndDeadline, f.load(t, deadline.ID).Reason)
	assert.Equal(t, SSHEndedByHub, f.load(t, expired.ID).EndedBy)
	assert.Equal(t, SSHEndIdle, f.load(t, idle.ID).Reason, "the owner's sessions were swept too")

	// The sweep's audit records each end once.
	require.NoError(t, AuditSSHSessionEnd(context.Background(), f.app.mongoClient, nil))
	for _, id := range []string{expired.ID, deadline.ID, idle.ID} {
		assert.True(t, f.load(t, id).EndAudited, id)
	}
	unaudited, err := f.app.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).
		CountDocuments(context.Background(), bson.M{"live": false, "end_audited": false})
	require.NoError(t, err)
	assert.Zero(t, unaudited)

	// Traffic keeps a session from going idle.
	view := f.mustCreate(t, f.others[1])
	f.set(t, view.ID, bson.M{"active_at": time.Now().UTC().Add(-SSHSessionIdle + time.Minute)})
	require.NoError(t, EndStaleSSHSessions(context.Background(), f.app.mongoClient, nil, time.Now().UTC()))
	assert.True(t, f.load(t, view.ID).Live)
}

func TestRenewSSHSession(t *testing.T) {
	f := newSSHFixture(t)
	view := f.mustCreate(t, f.connected)

	f.set(t, view.ID, bson.M{"expires_at": time.Now().UTC().Truncate(time.Second).Add(SSHSessionLease / 2)})
	rec := f.call(t, f.app.handleRenewSSHSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	renewal := SSHSessionRenewal{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &renewal))
	assert.WithinDuration(t, time.Now().Add(SSHSessionLease), renewal.ExpiresAt, 2*time.Second)
	assert.True(t, f.load(t, view.ID).ExpiresAt.Equal(renewal.ExpiresAt))

	deadline := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
	f.set(t, view.ID, bson.M{"deadline": deadline})
	rec = f.call(t, f.app.handleRenewSSHSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &renewal))
	assert.True(t, renewal.ExpiresAt.Equal(deadline), "never past the deadline")

	rec = f.call(t, f.app.handleRenewSSHSession, testStrangerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.call(t, f.app.handleRenewSSHSession, testOwnerPrn, http.MethodPost, f.others[0], view.ID, "/renew")
	assert.Equal(t, http.StatusNotFound, rec.Code, "a session is only found under its device")
	rec = f.call(t, f.app.handleRenewSSHSession, testOwnerPrn, http.MethodPost, f.connected, "nothex", "/renew")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	f.set(t, view.ID, bson.M{"expires_at": time.Now().UTC().Add(-time.Second)})
	rec = f.call(t, f.app.handleRenewSSHSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	assert.Equal(t, http.StatusGone, rec.Code)
	assert.Contains(t, rec.Body.String(), "expired")
	assert.True(t, f.load(t, view.ID).EndAudited, "reading an ended session audits its end")
}

func TestDeleteSSHSession(t *testing.T) {
	f := newSSHFixture(t)
	view := f.mustCreate(t, f.connected)

	rec := f.call(t, f.app.handleDeleteSSHSession, testStrangerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.True(t, f.load(t, view.ID).Live)

	rec = f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	stored := f.load(t, view.ID)
	assert.False(t, stored.Live)
	assert.Equal(t, SSHEndStopped, stored.Reason)
	assert.Equal(t, SSHEndedByHub, stored.EndedBy)
	assert.True(t, stored.EndAudited)

	rec = f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Equal(t, http.StatusNoContent, rec.Code, "stopping twice")
	rec = f.call(t, f.app.handleRenewSSHSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	assert.Equal(t, http.StatusGone, rec.Code)
}

func downFrame(t *testing.T, seq int64, data string, end bool, reason string) SSHFrame {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{"seq": seq, "data": []byte(data), "end": end, "reason": reason})
	require.NoError(t, err)
	frame, err := DecodeSSHFrame(raw)
	require.NoError(t, err)
	return frame
}

func (f *sshFixture) frames(t *testing.T, sid, dir string) []SSHFrame {
	t.Helper()
	oid, _ := primitive.ObjectIDFromHex(sid)
	cursor, err := f.app.mongoClient.Database(utils.MongoDb).Collection(SSHFramesCollection).Find(context.Background(),
		bson.M{"session": oid, "dir": dir})
	require.NoError(t, err)
	frames := []SSHFrame{}
	require.NoError(t, cursor.All(context.Background(), &frames))
	return frames
}

func (f *sshFixture) storeDown(device primitive.ObjectID, sid string, frame SSHFrame) error {
	oid, _ := primitive.ObjectIDFromHex(sid)
	return StoreSSHDownFrame(context.Background(), f.app.mongoClient, device, oid, frame, time.Now().UTC())
}

func TestStoreSSHDownFrame(t *testing.T) {
	f := newSSHFixture(t)
	view := f.mustCreate(t, f.connected)

	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, "", false, "")))
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 1, "SSH-2.0-dropbear\r\n", false, "")))
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 1, "SSH-2.0-dropbear\r\n", false, "")), "a redelivery")
	frames := f.frames(t, view.ID, SSHDirDown)
	require.Len(t, frames, 2, "stored once")
	stored := f.load(t, view.ID)
	assert.EqualValues(t, 2*len("SSH-2.0-dropbear\r\n"), stored.DownBytes, "a redelivery still counts")
	assert.EqualValues(t, 3, stored.DownFrames)
	assert.WithinDuration(t, time.Now(), stored.ActiveAt, 5*time.Second)

	assert.ErrorIs(t, f.storeDown(f.others[0], view.ID, downFrame(t, 2, "x", false, "")), ErrSSHSessionNoLive, "another device's session")
	assert.ErrorIs(t, f.storeDown(f.connected, primitive.NewObjectID().Hex(), downFrame(t, 2, "x", false, "")), ErrSSHSessionNoLive)

	// Stopped a moment ago: the frames so far went with the end, and the
	// device's last frame is still taken.
	f.call(t, f.app.handleDeleteSSHSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Empty(t, f.frames(t, view.ID, SSHDirDown), "the end deletes the frames")
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 2, "", true, SSHEndStopped)))
	assert.Equal(t, SSHEndStopped, f.load(t, view.ID).Reason)
	assert.Equal(t, SSHEndedByHub, f.load(t, view.ID).EndedBy, "the Hub's end stands")

	// Long over: nothing is taken.
	f.set(t, view.ID, bson.M{"ended_at": time.Now().UTC().Add(-SSHSessionGrace - time.Second)})
	assert.ErrorIs(t, f.storeDown(f.connected, view.ID, downFrame(t, 3, "late", false, "")), ErrSSHSessionNoLive)
	assert.Len(t, f.frames(t, view.ID, SSHDirDown), 1, "the frame within the grace stays for the TTL")

	// The device ends a session with its reason, "closed" by default.
	busy := f.mustCreate(t, f.others[0])
	require.NoError(t, f.storeDown(f.others[0], busy.ID, downFrame(t, 0, "", true, "busy")))
	ended := f.load(t, busy.ID)
	assert.False(t, ended.Live)
	assert.Equal(t, "busy", ended.Reason)
	assert.Equal(t, SSHEndedByDevice, ended.EndedBy)
	assert.True(t, ended.EndAudited)
	closed := f.mustCreate(t, f.others[0])
	require.NoError(t, f.storeDown(f.others[0], closed.ID, downFrame(t, 0, "", true, "")))
	assert.Equal(t, SSHEndClosed, f.load(t, closed.ID).Reason)
}

// A device above the rate has its session ended; one within it never does.
func TestSSHDownRate(t *testing.T) {
	f := newSSHFixture(t)
	view := f.mustCreate(t, f.connected)
	full := strings.Repeat("x", MaxSSHFrameData)
	// A window is more than the unattached allowance: a WebSocket is on.
	f.set(t, view.ID, bson.M{"attached": true})

	// A window's worth of full frames, as fast as they come.
	perWindow := int(sshRateWindowBytes / MaxSSHFrameData)
	for i := 0; i < perWindow; i++ {
		require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(i), full, false, "")), "frame %d", i)
	}
	assert.ErrorIs(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(perWindow), full, false, "")), ErrSSHSessionOverRate)
	ended := f.load(t, view.ID)
	assert.False(t, ended.Live)
	assert.Equal(t, SSHEndRate, ended.Reason)
	assert.Equal(t, SSHEndedByHub, ended.EndedBy, "the device is told to stop")

	// Once the window is over, it starts again.
	view = f.mustCreate(t, f.connected)
	f.set(t, view.ID, bson.M{
		"down_window_at":     time.Now().UTC().Add(-sshRateWindow - time.Second),
		"down_window_bytes":  sshRateWindowBytes,
		"down_window_frames": sshRateWindowFrames,
	})
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, full, false, "")))
	stored := f.load(t, view.ID)
	assert.EqualValues(t, MaxSSHFrameData, stored.DownWindowBytes)
	assert.EqualValues(t, 1, stored.DownWindowFrames)

	// Tiny frames are capped by count.
	f.set(t, view.ID, bson.M{"down_window_frames": sshRateWindowFrames})
	assert.ErrorIs(t, f.storeDown(f.connected, view.ID, downFrame(t, 1, "x", false, "")), ErrSSHSessionOverRate)

	// SSHRate sustained stays within the check.
	assert.GreaterOrEqual(t, sshRateWindowBytes, int64(SSHRate*sshRateWindow/time.Second))
}

// A device streaming while no WebSocket is attached gets SSHUnattachedBytes,
// then its session ends; attached, the count does not apply and goes back to
// zero.
func TestSSHDownUnattached(t *testing.T) {
	f := newSSHFixture(t)
	full := strings.Repeat("x", MaxSSHFrameData)
	perAllowance := int(SSHUnattachedBytes / MaxSSHFrameData)
	require.GreaterOrEqual(t, perAllowance, 2, "the allowance holds a banner and a key exchange")

	// The allowance is taken in full; a byte more ends the session.
	view := f.mustCreate(t, f.connected)
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, "", false, "")), "the ready frame")
	for i := 0; i < perAllowance; i++ {
		require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(i+1), full, false, "")), "frame %d", i+1)
	}
	assert.EqualValues(t, SSHUnattachedBytes, f.load(t, view.ID).UnattachedBytes)
	assert.ErrorIs(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(perAllowance+1), "x", false, "")), ErrSSHSessionUnattached)
	ended := f.load(t, view.ID)
	assert.False(t, ended.Live)
	assert.Equal(t, SSHEndUnattached, ended.Reason)
	assert.Equal(t, SSHEndedByHub, ended.EndedBy, "the device is told to stop")
	assert.True(t, ended.EndAudited)
	assert.Empty(t, f.frames(t, view.ID, SSHDirDown), "nothing is kept for a client that is not there")

	// At the allowance, the device's last frame (no data) is still taken.
	view = f.mustCreate(t, f.connected)
	f.set(t, view.ID, bson.M{"unattached_bytes": SSHUnattachedBytes})
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, "", true, "busy")))
	assert.Equal(t, "busy", f.load(t, view.ID).Reason)

	// Attached, the allowance does not apply, and the count is reset.
	view = f.mustCreate(t, f.connected)
	f.set(t, view.ID, bson.M{"unattached_bytes": SSHUnattachedBytes, "attached": true})
	for i := 0; i <= perAllowance; i++ {
		require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(i), full, false, "")), "frame %d", i)
	}
	stored := f.load(t, view.ID)
	assert.True(t, stored.Live)
	assert.Zero(t, stored.UnattachedBytes)

	// Without a WebSocket again (an upgrade that failed), it counts from zero.
	f.set(t, view.ID, bson.M{"attached": false})
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(perAllowance+1), full, false, "")))
	assert.EqualValues(t, MaxSSHFrameData, f.load(t, view.ID).UnattachedBytes)
	assert.True(t, f.load(t, view.ID).Live)

	// A session from before the count existed counts from zero too.
	oid, _ := primitive.ObjectIDFromHex(view.ID)
	_, err := f.app.mongoClient.Database(utils.MongoDb).Collection(SSHSessionsCollection).UpdateOne(context.Background(),
		bson.M{"_id": oid}, bson.M{"$unset": bson.M{"unattached_bytes": ""}})
	require.NoError(t, err)
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, int64(perAllowance+2), "hello", false, "")))
	assert.EqualValues(t, len("hello"), f.load(t, view.ID).UnattachedBytes)
}

// Ending a session deletes its frames, both directions, and nobody else's.
func TestEndSSHSessionDeletesFrames(t *testing.T) {
	f := newSSHFixture(t)
	view := f.mustCreate(t, f.connected)
	other := f.mustCreate(t, f.others[0])
	for _, s := range []struct {
		device primitive.ObjectID
		id     string
	}{{f.connected, view.ID}, {f.others[0], other.ID}} {
		require.NoError(t, f.storeDown(s.device, s.id, downFrame(t, 0, "", false, "")))
		require.NoError(t, f.storeDown(s.device, s.id, downFrame(t, 1, "SSH-2.0-dropbear\r\n", false, "")))
		session := f.load(t, s.id)
		require.NoError(t, storeSSHUpFrame(context.Background(), f.app.mongoClient, &session, 0, []byte("SSH-2.0-browser\r\n"), time.Now().UTC()))
	}
	require.Len(t, f.frames(t, view.ID, SSHDirDown), 2)
	require.Len(t, f.frames(t, view.ID, SSHDirUp), 1)

	oid, _ := primitive.ObjectIDFromHex(view.ID)
	now := time.Now().UTC()
	ended, err := EndSSHSession(context.Background(), f.app.mongoClient, oid, f.connected, SSHEndedByHub, SSHEndStopped, now)
	require.NoError(t, err)
	assert.True(t, ended)
	assert.Empty(t, f.frames(t, view.ID, SSHDirDown))
	assert.Empty(t, f.frames(t, view.ID, SSHDirUp))
	assert.Len(t, f.frames(t, other.ID, SSHDirDown), 2, "another session keeps its frames")
	assert.Len(t, f.frames(t, other.ID, SSHDirUp), 1)
	stored := f.load(t, view.ID)
	assert.False(t, stored.Live)
	assert.Equal(t, SSHEndStopped, stored.Reason)
	assert.True(t, stored.EndAudited)

	// A frame within the grace is taken again, for the WebSocket or the TTL;
	// ending a session that has ended does nothing, to it or to its frames.
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 2, "", true, "")))
	ended, err = EndSSHSession(context.Background(), f.app.mongoClient, oid, f.connected, SSHEndedByHub, SSHEndIdle, now)
	require.NoError(t, err)
	assert.False(t, ended)
	assert.Len(t, f.frames(t, view.ID, SSHDirDown), 1)
	assert.Equal(t, SSHEndStopped, f.load(t, view.ID).Reason)

	// The device's own end deletes them too, its end frame included: the
	// WebSocket, if any, has it from its change stream.
	require.NoError(t, f.storeDown(f.others[0], other.ID, downFrame(t, 2, "", true, "busy")))
	assert.Equal(t, "busy", f.load(t, other.ID).Reason)
	assert.Empty(t, f.frames(t, other.ID, SSHDirDown))
	assert.Empty(t, f.frames(t, other.ID, SSHDirUp))
}

func TestDecodeSSHFrame(t *testing.T) {
	frame, err := DecodeSSHFrame([]byte(`{"seq":3,"data":"aGVsbG8=","end":true,"reason":"` + strings.Repeat("r", 1000) + `"}`))
	require.NoError(t, err)
	assert.EqualValues(t, 3, frame.Seq)
	assert.Equal(t, []byte("hello"), frame.Data)
	assert.True(t, frame.End)
	assert.LessOrEqual(t, len(frame.Reason), maxSSHReasonLength)

	ready, err := DecodeSSHFrame([]byte(`{"seq":0}`))
	require.NoError(t, err)
	assert.NotNil(t, ready.Data)
	assert.Empty(t, ready.Data)

	full := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, MaxSSHFrameData))
	_, err = DecodeSSHFrame([]byte(`{"seq":1,"data":"` + full + `"}`))
	require.NoError(t, err, "a full frame fits MaxSSHFramePayload")

	over := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, MaxSSHFrameData+1))
	for _, payload := range []string{
		``, `not json`, `[]`, `{}`, `{"data":"aGk="}`, `{"seq":-1}`, `{"seq":"1"}`, `{"seq":1,"data":"!!"}`,
		`{"seq":1,"data":"` + over + `"}`,
		`{"seq":1,"pad":"` + strings.Repeat("x", MaxSSHFramePayload) + `"}`,
	} {
		_, err := DecodeSSHFrame([]byte(payload))
		assert.ErrorIs(t, err, ErrSSHFrameInvalid, "%.60s", payload)
	}

	encoded, err := EncodeSSHFrame(&SSHFrame{Seq: 7, Data: []byte("hi")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"seq":7,"data":"aGk=","end":false,"reason":""}`, string(encoded))
	encoded, err = EncodeSSHFrame(&SSHFrame{Seq: 0})
	require.NoError(t, err)
	assert.JSONEq(t, `{"seq":0,"data":"","end":false,"reason":""}`, string(encoded))
}

// Deleting a device ends and audits its live sessions (the notifier sends the
// stop from the ended document) and drops their frames and limits; the
// session documents stay as the audit record.
func TestEndDeviceSSHSessions(t *testing.T) {
	f := newSSHFixture(t)
	view := f.mustCreate(t, f.connected)
	require.NoError(t, f.storeDown(f.connected, view.ID, downFrame(t, 0, "", false, "")))
	other := f.mustCreate(t, f.others[0])

	require.NoError(t, f.app.EndDeviceSSHSessions(context.Background(), f.connected))
	db := f.app.mongoClient.Database(utils.MongoDb)
	n, err := db.Collection(SSHFramesCollection).CountDocuments(context.Background(), bson.M{"device_id": f.connected})
	require.NoError(t, err)
	assert.Zero(t, n, "frames")
	n, err = db.Collection(SSHSessionLimitsCollection).CountDocuments(context.Background(), bson.M{"_id": f.connected})
	require.NoError(t, err)
	assert.Zero(t, n, "limits")

	ended := f.load(t, view.ID)
	assert.False(t, ended.Live)
	assert.Equal(t, SSHEndedByHub, ended.EndedBy)
	assert.Equal(t, SSHEndStopped, ended.Reason)
	assert.NotNil(t, ended.EndedAt)
	assert.True(t, ended.EndAudited, "the end is audited")
	assert.True(t, f.load(t, other.ID).Live, "other devices keep theirs")

	// Again: nothing live is left, and nothing fails.
	require.NoError(t, f.app.EndDeviceSSHSessions(context.Background(), f.connected))
}

func TestSSHSessionIndexes(t *testing.T) {
	f := newSSHFixture(t)
	db := f.app.mongoClient.Database(utils.MongoDb)
	frames := db.Collection(SSHFramesCollection)
	check := func() {
		t.Helper()
		specs, err := frames.Indexes().ListSpecifications(context.Background())
		require.NoError(t, err)
		found := map[string]bool{}
		for _, spec := range specs {
			switch {
			case spec.Name == "created_at_1":
				require.NotNil(t, spec.ExpireAfterSeconds)
				assert.EqualValues(t, SSHFrameRetention.Seconds(), *spec.ExpireAfterSeconds, "frames are kept a minute at most")
				found["ttl"] = true
			case spec.Name == "session_1_dir_1_seq_1":
				require.NotNil(t, spec.Unique)
				assert.True(t, *spec.Unique)
				found["unique"] = true
			}
		}
		assert.True(t, found["ttl"] && found["unique"], "%v", found)
	}
	check()

	// A frames TTL index from before the retention changed is updated.
	_, err := frames.Indexes().DropOne(context.Background(), "created_at_1")
	require.NoError(t, err)
	_, err = frames.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: int32(1)}},
		Options: options.Index().SetExpireAfterSeconds(600),
	})
	require.NoError(t, err)
	require.NoError(t, f.app.EnsureSSHSessionIndices())
	check()
}
