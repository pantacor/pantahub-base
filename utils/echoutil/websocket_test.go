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

package echoutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// testTicket is shaped as an issued ticket: 43 characters of base64url.
const testTicket = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"

// The ticket offered as the subprotocol reaches the handler through the
// context, only on a WebSocket upgrade, and nothing else authenticates.
func TestWebSocketTicket(t *testing.T) {
	type outcome struct {
		code     int
		ticket   interface{}
		protocol interface{}
	}
	run := func(upgrade bool, auth string, protocols ...string) outcome {
		out := outcome{}
		e := echo.New()
		e.GET("/ws", func(c *echo.Context) error {
			out.ticket = c.Get(KeyWebSocketTicket)
			out.protocol = c.Get(KeyWebSocketProtocol)
			return c.NoContent(http.StatusNoContent)
		}, WebSocketTicket())
		req := httptest.NewRequest(http.MethodGet, "/ws", nil)
		if upgrade {
			req.Header.Set("Connection", "keep-alive, Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		for _, p := range protocols {
			req.Header.Add("Sec-WebSocket-Protocol", p)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		out.code = rec.Code
		if rec.Code == http.StatusUnauthorized && strings.Contains(strings.ToLower(rec.Header().Get("WWW-Authenticate")), "basic") {
			t.Errorf("refusal asks for Basic credentials: %q", rec.Header().Get("WWW-Authenticate"))
		}
		return out
	}

	got := run(true, "", "ticket."+testTicket)
	if got.code != http.StatusNoContent || got.ticket != testTicket || got.protocol != "ticket."+testTicket {
		t.Errorf("valid ticket: %+v", got)
	}
	other := strings.Repeat("x", 43)
	got = run(true, "", "chat, ticket."+testTicket+", ticket."+other)
	if got.code != http.StatusNoContent || got.ticket != testTicket || got.protocol != "ticket."+testTicket {
		t.Errorf("the first ticket of a list: %+v", got)
	}
	got = run(true, "", "ticket.short, ticket."+testTicket)
	if got.code != http.StatusNoContent || got.ticket != testTicket {
		t.Errorf("a malformed offer before the ticket: %+v", got)
	}
	for _, malformed := range []string{"ticket.", "ticket.short", "ticket." + testTicket + "=", "ticket." + testTicket[:42] + "+", "Ticket." + testTicket} {
		if got = run(true, "", malformed); got.code != http.StatusNoContent || got.ticket != nil || got.protocol != nil {
			t.Errorf("malformed %q reached the handler as a ticket: %+v", malformed, got)
		}
	}
	if got = run(true, ""); got.code != http.StatusNoContent || got.ticket != nil {
		t.Errorf("no offer: %+v", got)
	}
	if got = run(false, "", "ticket."+testTicket); got.code != http.StatusNoContent || got.ticket != nil {
		t.Errorf("not an upgrade, yet the subprotocol was taken: %+v", got)
	}

	// A bearer token in the handshake would be echoed back in the 101, so
	// the offer is refused, with or without a ticket beside it.
	if got = run(true, "", "bearer.eyJhbGciOiJIUzI1NiJ9.x.y"); got.code != http.StatusUnauthorized || got.ticket != nil {
		t.Errorf("bearer offer: %+v", got)
	}
	if got = run(true, "", "Bearer.eyJhbGciOiJIUzI1NiJ9.x.y, ticket."+testTicket); got.code != http.StatusUnauthorized || got.ticket != nil {
		t.Errorf("bearer offer beside a ticket: %+v", got)
	}
	// An Authorization header, which a browser may attach on its own (Basic
	// with a cached personal token), is refused whatever the subprotocol says.
	if got = run(true, "Bearer eyJhbGciOiJIUzI1NiJ9.x.y", "ticket."+testTicket); got.code != http.StatusUnauthorized || got.ticket != nil {
		t.Errorf("bearer header and ticket: %+v", got)
	}
	if got = run(true, "Bearer eyJhbGciOiJIUzI1NiJ9.x.y"); got.code != http.StatusUnauthorized {
		t.Errorf("bearer header without subprotocol: %+v", got)
	}
	if got = run(true, "Basic dXNlcjpwYXQ=", "ticket."+testTicket); got.code != http.StatusUnauthorized {
		t.Errorf("basic header: %+v", got)
	}
	if got = run(false, "Bearer eyJhbGciOiJIUzI1NiJ9.x.y"); got.code != http.StatusUnauthorized {
		t.Errorf("bearer header on a plain GET: %+v", got)
	}
}

// The ticket leaves the request headers once it is in the context; other
// offers stay; cookies never reach the handler; a refusal never asks for
// Basic credentials.
func TestWebSocketTicketScrubsRequest(t *testing.T) {
	var seen http.Header
	e := echo.New()
	e.GET("/ws", func(c *echo.Context) error {
		seen = c.Request().Header.Clone()
		return c.NoContent(http.StatusNoContent)
	}, WebSocketTicket())

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Add("Sec-WebSocket-Protocol", "ticket."+testTicket+", chat")
	req.Header.Add("Sec-WebSocket-Protocol", "ticket.other")
	req.Header.Set("Cookie", "session=abc")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if got := seen.Values("Sec-WebSocket-Protocol"); len(got) != 1 || got[0] != "chat" {
		t.Errorf("Sec-WebSocket-Protocol left: %q", got)
	}
	if seen.Get("Cookie") != "" {
		t.Errorf("cookie reached the handler: %q", seen.Get("Cookie"))
	}

	req = httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Authorization", "Basic dXNlcjpwYXQ=")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("basic: status %d", rec.Code)
	}
	if challenge := rec.Header().Get("WWW-Authenticate"); strings.Contains(strings.ToLower(challenge), "basic") {
		t.Errorf("refusal asks for Basic credentials: %q", challenge)
	}
}

// The fluent access log record of a WebSocket handshake holds no ticket, and
// no refused bearer token: not in the headers, not anywhere else in the
// record.
func TestWebSocketTicketAccessLogHasNoTicket(t *testing.T) {
	const bearer = "eyJhbGciOiJIUzI1NiJ9.secret.signature"

	var records []*utils.AccessLogFluentRecord
	mw := &utils.AccessLogFluentMiddleware{Prefix: "svc"}
	e := New()
	e.Pre(accessLogFluent(mw, "/svc", func(r *utils.AccessLogFluentRecord) { records = append(records, r) }),
		Instrument(), Recover(), WebSocketTicket())
	e.GET("/svc/ws", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

	for _, protocols := range []string{"ticket." + testTicket, "chat, ticket." + testTicket, "ticket.not-a-valid-ticket", "bearer." + bearer} {
		req := httptest.NewRequest(http.MethodGet, "/svc/ws", nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Protocol", protocols)
		e.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(records) != 4 {
		t.Fatalf("%d records", len(records))
	}
	for i, record := range records {
		b, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), testTicket) || strings.Contains(string(b), "not-a-valid-ticket") || strings.Contains(string(b), bearer) {
			t.Errorf("record %d holds the credential: %s", i, b)
		}
	}
	if records[0].StatusCode != http.StatusNoContent || records[3].StatusCode != http.StatusUnauthorized {
		t.Errorf("status codes %d, %d", records[0].StatusCode, records[3].StatusCode)
	}
}
