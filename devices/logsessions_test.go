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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestValidateLogSessionRequest(t *testing.T) {
	req, err := ValidateLogSessionRequest(LogSessionRequest{
		Sources: []string{"pantavisor/pantavisor.log", "telegraf/lxc/console.log", "pantavisor/pantavisor.log"},
		Tail:    200, Follow: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "current", req.Rev, "the revision defaults to current")
	assert.Equal(t, []string{"pantavisor/pantavisor.log", "telegraf/lxc/console.log"}, req.Sources, "duplicates dropped")

	for _, ok := range []LogSessionRequest{
		{Rev: "locals/hub-3", Sources: []string{""}},
		{Rev: "12", Sources: []string{"telegraf/"}, Tail: MaxLogTail},
		{Rev: "current", Sources: []string{"os/var/log/messages", "a..b.log"}, Tail: 0},
		{Sources: make([]string, MaxLogSources)},
		{Sources: []string{"x"}, Filter: "error"},
		{Sources: []string{"x"}, Filter: "[ctrl]: état=\"ok\" 100%"},
		{Sources: []string{"x"}, Filter: strings.Repeat("f", MaxLogFilterLength)},
		{Sources: []string{"x"}, Filter: strings.Repeat("é", MaxLogFilterLength/2)},
	} {
		_, err := ValidateLogSessionRequest(ok)
		assert.NoError(t, err, "%+v", ok)
	}

	tooMany := make([]string, MaxLogSources+1)
	for i := range tooMany {
		tooMany[i] = "src" + strconv.Itoa(i)
	}
	for _, bad := range []LogSessionRequest{
		{Sources: nil},
		{Sources: []string{}},
		{Sources: tooMany},
		{Sources: []string{"/etc/shadow"}},
		{Sources: []string{".."}},
		{Sources: []string{"../../etc/shadow"}},
		{Sources: []string{"pantavisor/../../x"}},
		{Sources: []string{"pantavisor/.."}},
		{Sources: []string{"a\x00b"}},
		{Sources: []string{"a\nb"}},
		{Sources: []string{`..\..\x`}},
		{Sources: []string{strings.Repeat("a", maxLogSourceLength+1)}},
		{Rev: "/current", Sources: []string{"x"}},
		{Rev: "../current", Sources: []string{"x"}},
		{Rev: "locals/..", Sources: []string{"x"}},
		{Rev: "locals//x", Sources: []string{"x"}},
		{Rev: "locals/", Sources: []string{"x"}},
		{Rev: "cur\trent", Sources: []string{"x"}},
		{Rev: strings.Repeat("r", maxLogRevLength+1), Sources: []string{"x"}},
		{Sources: []string{"x"}, Tail: -1},
		{Sources: []string{"x"}, Tail: MaxLogTail + 1},
		{Sources: []string{"x"}, Filter: strings.Repeat("f", MaxLogFilterLength+1)},
		{Sources: []string{"x"}, Filter: strings.Repeat("é", MaxLogFilterLength/2) + "é"},
		{Sources: []string{"x"}, Filter: "err\x00or"},
		{Sources: []string{"x"}, Filter: "err\nor"},
		{Sources: []string{"x"}, Filter: "err\tor"},
		{Sources: []string{"x"}, Filter: "err\x1b[0mor"},
		{Sources: []string{"x"}, Filter: "err\x7for"},
		{Sources: []string{"x"}, Filter: "err\u0085or"},
		{Sources: []string{"x"}, Filter: "err\xffor"},
		{Sources: []string{"x"}, Filter: "err\xc3or"},
	} {
		_, err := ValidateLogSessionRequest(bad)
		assert.Error(t, err, "%+v", bad)
	}
}

func TestLogSessionScopes(t *testing.T) {
	scopes := utils.MarshalScopes(LogSessionScopes)
	for scope, want := range map[string]bool{
		utils.Scopes.API.String():        true,
		utils.Scopes.DeviceLogs.String(): true,
		// Device access alone does not stream the device's logs.
		utils.Scopes.Devices.String():        false,
		utils.Scopes.WriteDevices.String():   false,
		utils.Scopes.ReadDevices.String():    false,
		utils.Scopes.DeviceCommands.String(): false,
		utils.Scopes.APIReadOnly.String():    false,
	} {
		assert.Equal(t, want, utils.MatchScope(scopes, []string{scope}), scope)
	}

	read := utils.MarshalScopes(readLogSessionScopes([]utils.Scope{utils.Scopes.API, utils.Scopes.ReadDevices}))
	for scope, want := range map[string]bool{
		utils.Scopes.ReadDevices.String():  true,
		utils.Scopes.DeviceLogs.String():   true,
		utils.Scopes.WriteObjects.String(): false,
	} {
		assert.Equal(t, want, utils.MatchScope(read, []string{scope}), scope)
	}
}

type logFixture struct {
	*commandFixture
	// more connected devices of the owner
	others []primitive.ObjectID
}

func newLogFixture(t *testing.T) *logFixture {
	t.Helper()
	f := &logFixture{commandFixture: newCommandFixture(t)}
	require.NoError(t, f.app.EnsureLogSessionIndices())
	// Idempotent.
	require.NoError(t, f.app.EnsureLogSessionIndices())

	devices := f.app.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")
	for i := 0; i < 3; i++ {
		id := primitive.NewObjectID()
		meta := map[string]interface{}{
			DeviceMetaMqttConnected: true, DeviceMetaMqttBroker: "live", DeviceMetaMqttConnection: "live/" + id.Hex(),
		}
		_, err := devices.InsertOne(context.Background(), bson.M{
			"_id": id, "prn": "prn:::devices:/" + id.Hex(), "nick": "dev_" + id.Hex(), "owner": testOwnerPrn,
			"device-meta": utils.BsonQuoteMap(&meta),
		})
		require.NoError(t, err)
		f.others = append(f.others, id)
	}
	return f
}

const logBody = `{"rev":"current","sources":["pantavisor/pantavisor.log"],"tail":10,"follow":true}`

func (f *logFixture) create(t *testing.T, caller string, device primitive.ObjectID, body string) (int, LogSessionView, string) {
	t.Helper()
	rec := testRequest(t, f.app.handlePostLogSession, caller, http.MethodPost, "/devices/"+device.Hex()+"/log-sessions",
		strings.NewReader(body), echo.PathValue{Name: "id", Value: device.Hex()})
	view := LogSessionView{}
	if rec.Code == http.StatusCreated {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	}
	return rec.Code, view, rec.Body.String()
}

func (f *logFixture) call(t *testing.T, handler func(*echo.Context) error, caller, method string, device primitive.ObjectID, sid, suffix string) *httptest.ResponseRecorder {
	t.Helper()
	return testRequest(t, handler, caller, method, "/devices/"+device.Hex()+"/log-sessions/"+sid+suffix, nil,
		echo.PathValue{Name: "id", Value: device.Hex()}, echo.PathValue{Name: "sid", Value: sid})
}

func (f *logFixture) load(t *testing.T, id string) LogSession {
	t.Helper()
	oid, err := primitive.ObjectIDFromHex(id)
	require.NoError(t, err)
	session := LogSession{}
	require.NoError(t, f.app.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection).
		FindOne(context.Background(), bson.M{"_id": oid}).Decode(&session))
	return session
}

func (f *logFixture) setTimes(t *testing.T, id string, set bson.M) {
	t.Helper()
	oid, err := primitive.ObjectIDFromHex(id)
	require.NoError(t, err)
	_, err = f.app.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection).
		UpdateOne(context.Background(), bson.M{"_id": oid}, bson.M{"$set": set})
	require.NoError(t, err)
}

func TestPostLogSession(t *testing.T) {
	f := newLogFixture(t)

	code, view, body := f.create(t, testOwnerPrn, f.connected, `{"sources":["pantavisor/pantavisor.log","telegraf/lxc/console.log"],"tail":200,"follow":true,"filter":"Error"}`)
	require.Equal(t, http.StatusCreated, code, body)
	assert.Equal(t, f.connected.Hex(), view.DeviceID)
	assert.Equal(t, "current", view.Rev)
	assert.Equal(t, []string{"pantavisor/pantavisor.log", "telegraf/lxc/console.log"}, view.Sources)
	assert.Equal(t, 200, view.Tail)
	assert.True(t, view.Follow)
	assert.Equal(t, "Error", view.Filter, "the filter is returned as sent, the device matches case-insensitively")
	assert.False(t, view.Ended)
	assert.Equal(t, LogSessionLease, view.ExpiresAt.Sub(view.CreatedAt))
	assert.Equal(t, LogSessionMaxDuration, view.Deadline.Sub(view.CreatedAt))
	assert.Equal(t, testOwnerPrn, view.CreatedBy)
	assert.WithinDuration(t, time.Now(), view.CreatedAt, 5*time.Second)
	for _, key := range []string{`"id"`, `"expires_at"`, `"deadline"`} {
		assert.Contains(t, body, key)
	}
	assert.NotContains(t, body, `"owner"`)

	stored := f.load(t, view.ID)
	assert.True(t, stored.Live)
	assert.Equal(t, testOwnerPrn, stored.Owner)
	assert.Equal(t, testOwnerPrn, stored.CreatedBy, "the session is the audit record")
	assert.Equal(t, []string{"pantavisor/pantavisor.log", "telegraf/lxc/console.log"}, stored.Sources)
	assert.Equal(t, "Error", stored.Filter, "the filter is part of the audit record")

	// No filter is stored and returned as "".
	code, view, body = f.create(t, testOwnerPrn, f.connected, `{"sources":["pantavisor/pantavisor.log"]}`)
	require.Equal(t, http.StatusCreated, code, body)
	assert.Equal(t, "", view.Filter)
	assert.Contains(t, body, `"filter":""`)
	assert.Equal(t, "", f.load(t, view.ID).Filter)

	for body, want := range map[string]string{
		`{"sources":["/etc/shadow"]}`:           "absolute path",
		`{"sources":["../x"]}`:                  "contains ..",
		`{"sources":[]}`:                        "at least one source",
		`{"sources":["x"],"tail":501}`:          "tail must be between",
		`{"rev":"/x","sources":["x"]}`:          "must not start with /",
		`{"sources":["x"],"tail":"lots"}`:       "Error parsing",
		`{"sources":["x"],"filter":"a\u0000b"}`: "control character",
		`{"sources":["x"],"filter":"a\nb"}`:     "control character",
		`{"sources":["x"],"filter":"` + strings.Repeat("f", MaxLogFilterLength+1) + `"}`: "longer than 256 bytes",
		`{"sources":["x"],"filter":42}`: "Error parsing",
		`not json`:                      "Error parsing",
		`{"sources":["1","2","3","4","5","6","7","8","9","10","11"]}`: "at most 10 sources",
	} {
		code, _, got := f.create(t, testOwnerPrn, f.connected, body)
		assert.Equal(t, http.StatusBadRequest, code, body)
		assert.Contains(t, got, want, "the reason reaches the caller: %s", body)
	}

	code, _, body = f.create(t, testOwnerPrn, f.connected, `{"sources":["`+strings.Repeat("x", maxLogSessionRequestSize)+`"]}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, code, body)

	code, _, body = f.create(t, testOwnerPrn, f.offline, logBody)
	assert.Equal(t, http.StatusConflict, code, body)
	assert.Contains(t, body, "not connected over MQTT")

	code, _, body = f.create(t, testOwnerPrn, f.orphaned, logBody)
	assert.Equal(t, http.StatusConflict, code, "connected to a replica without heartbeat: %s", body)

	code, _, body = f.create(t, testStrangerPrn, f.connected, logBody)
	assert.Equal(t, http.StatusNotFound, code, body)

	code, _, body = f.create(t, testOwnerPrn, primitive.NewObjectID(), logBody)
	assert.Equal(t, http.StatusNotFound, code, body)

	// By nick and PRN too.
	for _, ref := range []string{"dev_" + f.others[0].Hex(), "prn:::devices:/" + f.others[1].Hex()} {
		rec := testRequest(t, f.app.handlePostLogSession, testOwnerPrn, http.MethodPost, "/devices/"+ref+"/log-sessions",
			strings.NewReader(logBody), echo.PathValue{Name: "id", Value: ref})
		assert.Equal(t, http.StatusCreated, rec.Code, ref)
	}
}

// testScopedRequest runs handler behind the scope filter the route uses, as a
// USER request of caller holding scopes.
func testScopedRequest(t *testing.T, handler func(*echo.Context) error, scopes []string, caller, method, target, body string, params ...echo.PathValue) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := echo.New().NewContext(req, rec)
	c.Set(echoutil.KeyJWTPayload, jwtgo.MapClaims{"prn": caller, "type": "USER"})
	c.Set(echoutil.KeyAuthInfo, utils.AuthInfo{Caller: utils.Prn(caller), CallerType: "USER", Scopes: scopes})
	c.SetPathValues(append(echo.PathValues{}, params...))
	require.NoError(t, handler(c))
	return rec
}

// The routes' scope filters: devices.logs or the full API scope opens a
// session; device scopes alone do not; a read scope reads the lines.
func TestLogSessionRouteScopes(t *testing.T) {
	f := newLogFixture(t)
	post := echoutil.ScopeFilter(LogSessionScopes, f.app.handlePostLogSession)
	read := echoutil.ScopeFilter(readLogSessionScopes([]utils.Scope{utils.Scopes.API, utils.Scopes.APIReadOnly, utils.Scopes.Devices, utils.Scopes.ReadDevices}), f.app.handleGetLogLines)
	id := echo.PathValue{Name: "id", Value: f.connected.Hex()}

	refused := [][]string{
		{utils.Scopes.Devices.String()},
		{utils.Scopes.WriteDevices.String(), utils.Scopes.ReadDevices.String()},
		{utils.Scopes.DeviceCommands.String()},
		{utils.Scopes.APIReadOnly.String()},
		nil,
	}
	for _, scopes := range refused {
		rec := testScopedRequest(t, post, scopes, testOwnerPrn, http.MethodPost, "/", logBody, id)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%v", scopes)
	}

	var sid string
	for _, scopes := range [][]string{{utils.Scopes.DeviceLogs.String()}, {utils.Scopes.API.String()}} {
		rec := testScopedRequest(t, post, scopes, testOwnerPrn, http.MethodPost, "/", logBody, id)
		require.Equal(t, http.StatusCreated, rec.Code, "%v: %s", scopes, rec.Body.String())
		view := LogSessionView{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		sid = view.ID
		f.setTimes(t, sid, bson.M{"live": false, "reason": "stopped"}) // lines return at once
	}

	for _, scopes := range [][]string{{utils.Scopes.ReadDevices.String()}, {utils.Scopes.DeviceLogs.String()}} {
		rec := testScopedRequest(t, read, scopes, testOwnerPrn, http.MethodGet, "/?after=-1", "", id, echo.PathValue{Name: "sid", Value: sid})
		assert.Equal(t, http.StatusOK, rec.Code, "%v: %s", scopes, rec.Body.String())
	}
	rec := testScopedRequest(t, read, []string{utils.Scopes.WriteObjects.String()}, testOwnerPrn, http.MethodGet, "/", "", id, echo.PathValue{Name: "sid", Value: sid})
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// A scope is no substitute for owning the device.
	rec = testScopedRequest(t, read, []string{utils.Scopes.API.String()}, testStrangerPrn, http.MethodGet, "/", "", id, echo.PathValue{Name: "sid", Value: sid})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Requests racing each other, on any replica, cannot open more live sessions
// than the limits together.
func TestLogSessionLimitsAreAtomic(t *testing.T) {
	f := newLogFixture(t)

	race := func(n int, device primitive.ObjectID) (created, limited int) {
		codes := make(chan int, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, _, _ := f.create(t, testOwnerPrn, device, logBody)
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
				limited++
			default:
				t.Errorf("unexpected status %d", code)
			}
		}
		return created, limited
	}

	// Within the start rate limit (logStartRateLimit per minute), so every
	// refusal here is the live cap.
	created, limited := race(5, f.connected)
	assert.Equal(t, MaxLogSessionsPerDevice, created, "sessions opened at once on one device")
	assert.Equal(t, 5-MaxLogSessionsPerDevice, limited)

	code, _, body := f.create(t, testOwnerPrn, f.connected, logBody)
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Contains(t, body, "this device already streams", "the reason reaches the caller")

	// 2 on the first device, 2 on the second, 1 on the third: the user's 5.
	created, _ = race(6, f.others[0])
	assert.Equal(t, MaxLogSessionsPerDevice, created)
	created, _ = race(6, f.others[1])
	assert.Equal(t, 1, created, "the user's limit")
	code, _, body = f.create(t, testOwnerPrn, f.others[2], logBody)
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Contains(t, body, "you already have")

	live, err := f.app.mongoClient.Database(utils.MongoDb).Collection(LogSessionsCollection).
		CountDocuments(context.Background(), bson.M{"owner": testOwnerPrn, "live": true})
	require.NoError(t, err)
	assert.EqualValues(t, MaxLogSessionsPerUser, live)
}

// A session whose client stopped renewing ends, and frees its slot, without
// anybody stopping it.
func TestLogSessionExpiry(t *testing.T) {
	f := newLogFixture(t)

	ids := []string{}
	for i := 0; i < MaxLogSessionsPerDevice; i++ {
		code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
		require.Equal(t, http.StatusCreated, code, body)
		ids = append(ids, view.ID)
	}
	code, _, _ := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusTooManyRequests, code)

	// The first one's tab was closed a minute ago, the second hit its deadline.
	past := time.Now().UTC().Add(-time.Second)
	f.setTimes(t, ids[0], bson.M{"expires_at": past})
	f.setTimes(t, ids[1], bson.M{"expires_at": past, "deadline": past})

	code, _, body := f.create(t, testOwnerPrn, f.connected, logBody)
	assert.Equal(t, http.StatusCreated, code, "stale sessions free their slots: %s", body)
	assert.Equal(t, "expired", f.load(t, ids[0]).Reason)
	assert.Equal(t, "deadline", f.load(t, ids[1]).Reason)
	assert.False(t, f.load(t, ids[0]).Live)
	assert.Equal(t, LogEndedByHub, f.load(t, ids[0]).EndedBy)

	// The sweep ends them with nobody asking.
	code, view, _ := f.create(t, testOwnerPrn, f.others[0], logBody)
	require.Equal(t, http.StatusCreated, code)
	f.setTimes(t, view.ID, bson.M{"expires_at": past})
	require.NoError(t, EndStaleLogSessions(context.Background(), f.app.mongoClient, nil, time.Now().UTC()))
	swept := f.load(t, view.ID)
	assert.False(t, swept.Live)
	assert.Equal(t, "expired", swept.Reason)
	assert.NotNil(t, swept.EndedAt)

	// Reading it ends it too.
	code, view, _ = f.create(t, testOwnerPrn, f.others[1], logBody)
	require.Equal(t, http.StatusCreated, code)
	f.setTimes(t, view.ID, bson.M{"expires_at": past})
	rec := f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.others[1], view.ID, "/renew")
	assert.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "expired")
}

func TestRenewLogSession(t *testing.T) {
	f := newLogFixture(t)
	code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)

	// Half a lease later.
	f.setTimes(t, view.ID, bson.M{"expires_at": time.Now().UTC().Truncate(time.Second).Add(LogSessionLease / 2)})
	rec := f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	renewal := LogSessionRenewal{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &renewal))
	assert.WithinDuration(t, time.Now().Add(LogSessionLease), renewal.ExpiresAt, 2*time.Second)
	assert.Equal(t, view.Deadline, renewal.Deadline)
	assert.True(t, f.load(t, view.ID).ExpiresAt.Equal(renewal.ExpiresAt), "stored as returned")

	// Near the deadline the lease stops at it.
	deadline := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
	f.setTimes(t, view.ID, bson.M{"deadline": deadline})
	rec = f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &renewal))
	assert.True(t, renewal.ExpiresAt.Equal(deadline), "renewed to %v, deadline %v", renewal.ExpiresAt, deadline)

	rec = f.call(t, f.app.handleRenewLogSession, testStrangerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.others[0], view.ID, "/renew")
	assert.Equal(t, http.StatusNotFound, rec.Code, "a session is only found under its device")
	rec = f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.connected, primitive.NewObjectID().Hex(), "/renew")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.connected, "nothex", "/renew")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeleteLogSession(t *testing.T) {
	f := newLogFixture(t)
	code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)

	rec := f.call(t, f.app.handleDeleteLogSession, testStrangerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.True(t, f.load(t, view.ID).Live)

	rec = f.call(t, f.app.handleDeleteLogSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	stored := f.load(t, view.ID)
	assert.False(t, stored.Live)
	assert.Equal(t, LogEndStopped, stored.Reason)
	assert.Equal(t, LogEndedByHub, stored.EndedBy)

	rec = f.call(t, f.app.handleDeleteLogSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	assert.Equal(t, http.StatusNoContent, rec.Code, "stopping twice")
	assert.Equal(t, LogEndStopped, f.load(t, view.ID).Reason)

	rec = f.call(t, f.app.handleRenewLogSession, testOwnerPrn, http.MethodPost, f.connected, view.ID, "/renew")
	assert.Equal(t, http.StatusGone, rec.Code)
}

func storeBatch(t *testing.T, f *logFixture, device primitive.ObjectID, sid string, seq int64, end bool, lines ...string) error {
	t.Helper()
	payload := map[string]interface{}{"session": sid, "seq": seq, "end": end, "reason": "", "dropped": 0}
	wire := []map[string]string{}
	for _, line := range lines {
		wire = append(wire, map[string]string{"src": "pantavisor/pantavisor.log", "line": line})
	}
	payload["lines"] = wire
	if end {
		payload["reason"] = "done"
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	batch, err := DecodeLogBatch(raw)
	require.NoError(t, err)
	return StoreLogBatch(context.Background(), f.app.mongoClient, device, batch, time.Now().UTC())
}

func (f *logFixture) lines(t *testing.T, device primitive.ObjectID, sid, query string) (int, LogLinesView) {
	t.Helper()
	rec := f.call(t, f.app.handleGetLogLines, testOwnerPrn, http.MethodGet, device, sid, "/lines"+query)
	view := LogLinesView{}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	}
	return rec.Code, view
}

func withLongPoll(t *testing.T, wait time.Duration) {
	t.Helper()
	previousWait, previousInterval := logLinesWait, logLinesPollInterval
	logLinesWait, logLinesPollInterval = wait, 50*time.Millisecond
	t.Cleanup(func() { logLinesWait, logLinesPollInterval = previousWait, previousInterval })
}

func TestGetLogLines(t *testing.T) {
	f := newLogFixture(t)
	withLongPoll(t, 300*time.Millisecond)
	code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)

	// Nothing yet: the poll waits, then answers empty.
	started := time.Now()
	code, lines := f.lines(t, f.connected, view.ID, "")
	require.Equal(t, http.StatusOK, code)
	assert.GreaterOrEqual(t, time.Since(started), logLinesWait)
	assert.Empty(t, lines.Batches)
	assert.NotNil(t, lines.Batches, "an empty list, not null")
	assert.EqualValues(t, -1, lines.Next)
	assert.False(t, lines.Ended)

	for seq := int64(0); seq < 3; seq++ {
		require.NoError(t, storeBatch(t, f, f.connected, view.ID, seq, false, "line "+strconv.FormatInt(seq, 10)))
	}
	code, lines = f.lines(t, f.connected, view.ID, "?after=-1")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, lines.Batches, 3)
	assert.EqualValues(t, 2, lines.Next)
	assert.Equal(t, "line 0", lines.Batches[0].Lines[0].Line)
	assert.Equal(t, "pantavisor/pantavisor.log", lines.Batches[0].Lines[0].Src)

	code, lines = f.lines(t, f.connected, view.ID, "?after=0")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, lines.Batches, 2, "after is honoured")
	assert.EqualValues(t, 1, lines.Batches[0].Seq)

	// A batch arriving while the client waits is returned at once.
	withLongPoll(t, 20*time.Second)
	go func() {
		time.Sleep(300 * time.Millisecond)
		assert.NoError(t, storeBatch(t, f, f.connected, view.ID, 3, false, "late"))
	}()
	started = time.Now()
	code, lines = f.lines(t, f.connected, view.ID, "?after=2")
	require.Equal(t, http.StatusOK, code)
	assert.Less(t, time.Since(started), 5*time.Second, "returned when the batch arrived")
	require.Len(t, lines.Batches, 1)
	assert.Equal(t, "late", lines.Batches[0].Lines[0].Line)
	assert.EqualValues(t, 3, lines.Next)

	// Stopping ends a waiting poll too.
	go func() {
		time.Sleep(300 * time.Millisecond)
		f.call(t, f.app.handleDeleteLogSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	}()
	started = time.Now()
	code, lines = f.lines(t, f.connected, view.ID, "?after=3")
	require.Equal(t, http.StatusOK, code)
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.True(t, lines.Ended)
	assert.Equal(t, LogEndStopped, lines.Reason)
	assert.EqualValues(t, 3, lines.Next)

	// Once stopped, every later poll reports the end at once, with the
	// batches still unread: never a 410.
	withLongPoll(t, 20*time.Second)
	started = time.Now()
	code, lines = f.lines(t, f.connected, view.ID, "?after=-1")
	require.Equal(t, http.StatusOK, code)
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.True(t, lines.Ended)
	assert.Equal(t, LogEndStopped, lines.Reason)
	require.Len(t, lines.Batches, 4)
	assert.EqualValues(t, 0, lines.Batches[0].Seq, "the first batch is seq 0")
	assert.EqualValues(t, 3, lines.Next)

	for _, query := range []string{"?after=x", "?after=-2", "?after=1.5"} {
		code, _ = f.lines(t, f.connected, view.ID, query)
		assert.Equal(t, http.StatusBadRequest, code, query)
	}
	rec := f.call(t, f.app.handleGetLogLines, testStrangerPrn, http.MethodGet, f.connected, view.ID, "/lines")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A session holding more batches than a page is only reported ended once the
// client has read them all.
func TestGetLogLinesPages(t *testing.T) {
	f := newLogFixture(t)
	withLongPoll(t, 100*time.Millisecond)
	code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)

	total := int64(logLinesMaxBatches + 5)
	for seq := int64(0); seq < total; seq++ {
		require.NoError(t, storeBatch(t, f, f.connected, view.ID, seq, seq == total-1, "x"))
	}
	assert.False(t, f.load(t, view.ID).Live, "the device's last batch ends the session")

	code, lines := f.lines(t, f.connected, view.ID, "")
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, lines.Batches, logLinesMaxBatches)
	assert.False(t, lines.Ended, "more to read")

	code, lines = f.lines(t, f.connected, view.ID, "?after="+strconv.FormatInt(lines.Next, 10))
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, lines.Batches, 5)
	assert.True(t, lines.Ended)
	assert.Equal(t, "done", lines.Reason)
	assert.True(t, lines.Batches[4].End)
}

func TestStoreLogBatch(t *testing.T) {
	f := newLogFixture(t)
	code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)
	stream := f.app.mongoClient.Database(utils.MongoDb).Collection(LogStreamCollection)
	count := func() int64 {
		n, err := stream.CountDocuments(context.Background(), bson.M{})
		require.NoError(t, err)
		return n
	}

	require.NoError(t, storeBatch(t, f, f.connected, view.ID, 0, false, "a"))
	require.NoError(t, storeBatch(t, f, f.connected, view.ID, 0, false, "a"), "a redelivery is not an error")
	assert.EqualValues(t, 1, count(), "and is stored once")

	assert.ErrorIs(t, storeBatch(t, f, f.others[0], view.ID, 1, false, "forged"), ErrLogSessionNoLive, "another device's session")
	assert.ErrorIs(t, storeBatch(t, f, f.connected, primitive.NewObjectID().Hex(), 1, false, "x"), ErrLogSessionNoLive, "unknown session")
	assert.EqualValues(t, 1, count())

	// Stopped a moment ago: the device's last batch is still taken.
	f.call(t, f.app.handleDeleteLogSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
	require.NoError(t, storeBatch(t, f, f.connected, view.ID, 1, true, "last"))
	assert.Equal(t, LogEndStopped, f.load(t, view.ID).Reason, "the Hub's reason stands")

	// Long over: nothing is taken.
	f.setTimes(t, view.ID, bson.M{"ended_at": time.Now().UTC().Add(-LogSessionGrace - time.Second)})
	assert.ErrorIs(t, storeBatch(t, f, f.connected, view.ID, 2, false, "late"), ErrLogSessionNoLive)
	assert.EqualValues(t, 2, count())

	// Past its lease, never ended by the Hub: taken within the grace only.
	code, view, _ = f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code)
	f.setTimes(t, view.ID, bson.M{"expires_at": time.Now().UTC().Add(-LogSessionGrace - time.Second)})
	assert.ErrorIs(t, storeBatch(t, f, f.connected, view.ID, 0, false, "late"), ErrLogSessionNoLive)

	// The device ends a session with its reason.
	code, view, _ = f.create(t, testOwnerPrn, f.others[0], logBody)
	require.Equal(t, http.StatusCreated, code)
	require.NoError(t, storeBatch(t, f, f.others[0], view.ID, 0, true))
	ended := f.load(t, view.ID)
	assert.False(t, ended.Live)
	assert.Equal(t, "done", ended.Reason)
	assert.Equal(t, LogEndedByDevice, ended.EndedBy)
}

func TestDecodeLogBatch(t *testing.T) {
	sid := primitive.NewObjectID()
	long := strings.Repeat("é", maxLogLineLength/2+10)
	lines := make([]map[string]string, maxLogBatchLines+3)
	for i := range lines {
		lines[i] = map[string]string{"src": "s", "line": "l"}
	}
	lines[0]["line"] = long
	lines[1]["src"] = strings.Repeat("s", maxLogSourceLength+10)
	raw, err := json.Marshal(map[string]interface{}{"session": sid.Hex(), "seq": 4, "lines": lines, "dropped": 2, "end": true, "reason": strings.Repeat("r", 1000)})
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), MaxLogBatchSize)

	batch, err := DecodeLogBatch(raw)
	require.NoError(t, err)
	assert.Equal(t, sid, batch.Session)
	assert.EqualValues(t, 4, batch.Seq)
	assert.Len(t, batch.Lines, maxLogBatchLines)
	assert.EqualValues(t, 5, batch.Dropped, "lines past the cap are counted as dropped")
	assert.True(t, strings.HasSuffix(batch.Lines[0].Line, logTruncatedMarker))
	assert.LessOrEqual(t, len(batch.Lines[0].Line), maxLogLineLength+len(logTruncatedMarker))
	assert.True(t, utf8.ValidString(batch.Lines[0].Line))
	assert.LessOrEqual(t, len(batch.Lines[1].Src), maxLogSourceLength)
	assert.LessOrEqual(t, len(batch.Reason), maxLogReasonLength)

	small, err := DecodeLogBatch([]byte(`{"session":"` + sid.Hex() + `","seq":0}`))
	require.NoError(t, err)
	assert.NotNil(t, small.Lines)

	// A device-chosen session id is quoted in the error (which the bridge
	// logs) only up to maxLogQuotedLength.
	huge := `{"session":"` + strings.Repeat("\u2028x", MaxLogBatchSize/8) + `","seq":1}`
	require.LessOrEqual(t, len(huge), MaxLogBatchSize)
	_, err = DecodeLogBatch([]byte(huge))
	require.ErrorIs(t, err, ErrLogBatchInvalid)
	assert.Less(t, len(err.Error()), 4*maxLogQuotedLength+100, "error: %.200s", err.Error())
	assert.Contains(t, err.Error(), "bytes)")

	for _, payload := range []string{
		``, `not json`, `[]`,
		`{"seq":1}`,
		`{"session":"nothex","seq":1}`,
		`{"session":"` + sid.Hex() + `","seq":-1}`,
		`{"session":"` + sid.Hex() + `","seq":"1"}`,
		`{"session":"` + sid.Hex() + `","seq":1,"lines":"x"}`,
		`{"session":"` + sid.Hex() + `","seq":1,"lines":[],"pad":"` + strings.Repeat("x", MaxLogBatchSize) + `"}`,
	} {
		_, err := DecodeLogBatch([]byte(payload))
		assert.ErrorIs(t, err, ErrLogBatchInvalid, "%.60s", payload)
	}
}

func TestDeleteDeviceLogSessions(t *testing.T) {
	f := newLogFixture(t)
	code, view, _ := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code)
	require.NoError(t, storeBatch(t, f, f.connected, view.ID, 0, false, "a"))
	code, other, _ := f.create(t, testOwnerPrn, f.others[0], logBody)
	require.Equal(t, http.StatusCreated, code)

	require.NoError(t, f.app.DeleteDeviceLogSessions(context.Background(), f.connected))

	db := f.app.mongoClient.Database(utils.MongoDb)
	n, err := db.Collection(LogSessionsCollection).CountDocuments(context.Background(), bson.M{"device_id": f.connected})
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = db.Collection(LogStreamCollection).CountDocuments(context.Background(), bson.M{"device_id": f.connected})
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.True(t, f.load(t, other.ID).Live, "other devices keep theirs")
}

func TestLogSessionIndexes(t *testing.T) {
	f := newLogFixture(t)
	db := f.app.mongoClient.Database(utils.MongoDb)

	ttl := func(collection string) *int32 {
		specs, err := db.Collection(collection).Indexes().ListSpecifications(context.Background())
		require.NoError(t, err)
		for _, spec := range specs {
			if spec.Name == "created_at_1" {
				return spec.ExpireAfterSeconds
			}
		}
		return nil
	}
	require.NotNil(t, ttl(LogStreamCollection))
	assert.EqualValues(t, LogStreamRetention.Seconds(), *ttl(LogStreamCollection), "batches are kept 10 minutes")
	require.NotNil(t, ttl(LogSessionsCollection))
	assert.EqualValues(t, LogSessionRetention.Seconds(), *ttl(LogSessionsCollection))
}

// A start/stop loop is bounded per device, like commands, whatever the live
// caps allow.
func TestLogSessionStartRateLimit(t *testing.T) {
	f := newLogFixture(t)
	for i := range logStartRateLimit {
		code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
		require.Equal(t, http.StatusCreated, code, "start %d: %s", i, body)
		rec := f.call(t, f.app.handleDeleteLogSession, testOwnerPrn, http.MethodDelete, f.connected, view.ID, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
	}
	code, _, body := f.create(t, testOwnerPrn, f.connected, logBody)
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Contains(t, body, errLogStartRateLimited.Error())

	// Another device of the same owner is not affected.
	code, _, body = f.create(t, testOwnerPrn, f.others[0], logBody)
	assert.Equal(t, http.StatusCreated, code, body)
}

// A device flooding its own session stops being stored at the budget; its
// last batch still ends the session.
func TestStoreLogBatchBudget(t *testing.T) {
	f := newLogFixture(t)
	stream := f.app.mongoClient.Database(utils.MongoDb).Collection(LogStreamCollection)
	count := func(sid string) int64 {
		oid, _ := primitive.ObjectIDFromHex(sid)
		n, err := stream.CountDocuments(context.Background(), bson.M{"session": oid})
		require.NoError(t, err)
		return n
	}

	code, view, body := f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)
	require.NoError(t, storeBatch(t, f, f.connected, view.ID, 0, false, "a", "bb"))
	stored := f.load(t, view.ID)
	assert.EqualValues(t, 1, stored.StreamBatches)
	assert.EqualValues(t, logBatchOverhead+3+2*(len("pantavisor/pantavisor.log")+logLineOverhead), stored.StreamBytes)

	f.setTimes(t, view.ID, bson.M{"stream_batches": MaxLogSessionBatches})
	assert.ErrorIs(t, storeBatch(t, f, f.connected, view.ID, 1, false, "flood"), ErrLogSessionOverBudget)
	assert.EqualValues(t, 1, count(view.ID), "over the batch budget: not stored")
	assert.ErrorIs(t, storeBatch(t, f, f.connected, view.ID, 2, true, "last"), ErrLogSessionOverBudget)
	ended := f.load(t, view.ID)
	assert.False(t, ended.Live, "the last batch still ends the session")
	assert.Equal(t, LogEndedByDevice, ended.EndedBy)

	code, view, body = f.create(t, testOwnerPrn, f.connected, logBody)
	require.Equal(t, http.StatusCreated, code, body)
	f.setTimes(t, view.ID, bson.M{"stream_bytes": MaxLogSessionBytes})
	assert.ErrorIs(t, storeBatch(t, f, f.connected, view.ID, 0, false, "flood"), ErrLogSessionOverBudget)
	assert.EqualValues(t, 0, count(view.ID), "over the byte budget: not stored")
}

// Empty lines are not free, and a device honouring the contract never runs
// out of budget.
func TestLogBatchBytes(t *testing.T) {
	assert.EqualValues(t, logBatchOverhead, logBatchBytes(LogBatch{}), "an empty batch")
	empty := logBatchBytes(LogBatch{Lines: make([]LogLine, maxLogBatchLines)})
	assert.EqualValues(t, logBatchOverhead+maxLogBatchLines*logLineOverhead, empty, "a batch of empty lines")
	assert.Less(t, int64(MaxLogSessionBytes)/empty, int64(MaxLogSessionBatches),
		"batches of empty lines run out of byte budget before the batch budget")

	// The device charges each line len(line)+len(src)+16 against 64 KiB/s
	// with a 256 KiB burst, and sends at most 2 batches a second or one per
	// 32 KiB: over the longest session (deadline plus grace) that is what
	// the Hub counts at most.
	const deviceRate, deviceBurst, flushBytes = 64 << 10, 256 << 10, 32 << 10
	longest := int64((LogSessionMaxDuration + LogSessionGrace).Seconds())
	text := deviceRate*longest + deviceBurst
	batches := 2*longest + text/flushBytes
	assert.Less(t, text+batches*logBatchOverhead, int64(MaxLogSessionBytes))
	assert.Less(t, batches, int64(MaxLogSessionBatches))
}

// Across its devices, an owner's start/stop loop is bounded too.
func TestLogSessionOwnerStartRateLimit(t *testing.T) {
	f := newLogFixture(t)
	devices := append([]primitive.ObjectID{f.connected}, f.others...)
	require.Greater(t, len(devices)*logStartRateLimit, logOwnerStartRateLimit, "the fixture can pass the owner limit")
	for i := range logOwnerStartRateLimit {
		device := devices[i%len(devices)]
		code, view, body := f.create(t, testOwnerPrn, device, logBody)
		require.Equal(t, http.StatusCreated, code, "start %d: %s", i, body)
		rec := f.call(t, f.app.handleDeleteLogSession, testOwnerPrn, http.MethodDelete, device, view.ID, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
	}
	for _, device := range devices {
		code, _, body := f.create(t, testOwnerPrn, device, logBody)
		assert.Equal(t, http.StatusTooManyRequests, code, body)
		assert.Contains(t, body, errLogOwnerStartRateLimited.Error())
	}
}

func TestLogPollSlots(t *testing.T) {
	assert.GreaterOrEqual(t, maxLogPollsPerCaller, MaxLogSessionsPerUser, "a user may poll each of its live sessions at once")
	p := &pollSlots{}
	for range maxLogPollsPerCaller {
		require.True(t, p.acquire("u1"))
	}
	assert.False(t, p.acquire("u1"), "one poll too many")
	assert.True(t, p.acquire("u2"), "another caller is not affected")
	p.release("u1")
	assert.True(t, p.acquire("u1"), "a finished poll frees its slot")
	for range maxLogPollsPerCaller {
		p.release("u1")
	}
	p.release("u2")
	assert.Empty(t, p.n, "released callers are forgotten")
}
