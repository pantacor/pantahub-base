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

package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

// stubBackend returns a fixed page, standing in for Elasticsearch.
type stubBackend struct {
	entries    []*Entry
	nextCursor string
	gotCursor  bool
}

func (s *stubBackend) getLogs(_ context.Context, _ int64, _ int64, _ *time.Time, _ *time.Time,
	_ Filters, _ Sorts, _ []interface{}, cursor bool) (*Pager, error) {
	s.gotCursor = cursor
	return &Pager{Entries: s.entries, NextCursor: s.nextCursor}, nil
}

func (s *stubBackend) postLogs(context.Context, []Entry, bool) error { return nil }
func (s *stubBackend) register() error                               { return nil }
func (s *stubBackend) unregister(bool) error                         { return nil }

// The error paths log through fluentd, which is not running in a unit test;
// an explicitly empty port makes utils' logger a no-op (GetEnv uses LookupEnv,
// so an empty value wins over the built-in default).
func withoutFluent(t *testing.T) {
	t.Helper()
	t.Setenv(utils.EnvFluentPort, "")
}

func testApp(backend Backend) *App {
	return &App{
		backend: backend,
		jwtConfig: &jwtauth.Config{
			Key:              []byte("test-signing-key"),
			SigningAlgorithm: "HS256",
		},
	}
}

func getLogsRequest(t *testing.T, target string) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(echoutil.KeyJWTPayload, jwtgo.MapClaims{
		"type": "USER",
		"prn":  "prn:::accounts:/testowner",
	})
	return c, rec
}

// A caller that asks for a cursor must always be handed one back, including
// when the page is empty.
//
// Followers such as `pvr device logs` call the cursor endpoint with whatever
// they were last given. Dropping the cursor on an empty page left them posting
// an empty string, which cannot parse as a token and came back as a 403 --
// which those clients read as an expired session and answer by sending the
// user to a login prompt, mid-tail, on an idle device.
func TestHandleGetLogsAlwaysReturnsCursorWhenAsked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*Entry
		next    string
	}{
		{"page with entries", []*Entry{{LogText: "hello"}}, `[1789458134620,"6aa8f6d6cf81a60009704aee"]`},
		{"empty page", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := testApp(&stubBackend{entries: tc.entries, nextCursor: tc.next})

			c, recorder := getLogsRequest(t, "/logs/?cursor=true&page=50")
			_ = app.handleGetLogs(c)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
			}

			var pager Pager
			if err := json.Unmarshal(recorder.Body.Bytes(), &pager); err != nil {
				t.Fatalf("decoding response: %s", err)
			}

			if pager.NextCursor == "" {
				t.Fatal("cursor was requested but none was returned; a follower would post an empty cursor and be told to log in")
			}
		})
	}
}

// Without cursor=true there is nothing to continue from, so none is issued.
func TestHandleGetLogsOmitsCursorWhenNotAsked(t *testing.T) {
	app := testApp(&stubBackend{entries: []*Entry{{LogText: "hello"}}})

	c, recorder := getLogsRequest(t, "/logs/?page=50")
	_ = app.handleGetLogs(c)

	var pager Pager
	if err := json.Unmarshal(recorder.Body.Bytes(), &pager); err != nil {
		t.Fatalf("decoding response: %s", err)
	}
	if pager.NextCursor != "" {
		t.Errorf("no cursor was asked for, yet one came back: %q", pager.NextCursor)
	}
}

// An absent cursor is a bad request. Answering 403 made clients treat it as an
// authentication failure and bounce the user to a login prompt.
func TestHandleGetLogsCursorRejectsEmptyCursorAsBadRequest(t *testing.T) {
	withoutFluent(t)

	app := testApp(&stubBackend{})

	c, recorder := getLogsRequest(t, "/logs/cursor")
	_ = app.handleGetLogsCursor(c)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a missing cursor, got %d (403 reads as expired auth to clients)", recorder.Code)
	}
}
