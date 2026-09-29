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

package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Credentials never reach the fluent record: the credential headers are left
// out, and a session ticket or a bearer token offered as a WebSocket
// subprotocol is redacted whatever route it came on, the other offers kept.
func TestAccessLogFluentRecordRedactsCredentials(t *testing.T) {
	const token = "eyJhbGciOiJSUzI1NiJ9.secret.signature"
	const ticket = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"
	r := httptest.NewRequest(http.MethodGet, "/x/ws", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Proxy-Authorization", "Basic "+token)
	r.Header.Set("Cookie", "session="+token)
	r.Header.Add("Sec-WebSocket-Protocol", "chat, bearer."+token)
	r.Header.Add("Sec-WebSocket-Protocol", "Bearer."+token)
	r.Header.Add("Sec-WebSocket-Protocol", "ticket."+ticket+", chat")
	r.Header.Add("Sec-WebSocket-Protocol", "Ticket."+ticket)
	r.Header.Set("User-Agent", "test/1")

	start := time.Now()
	elapsed := time.Millisecond
	env := map[string]interface{}{"START_TIME": &start, "ELAPSED_TIME": &elapsed, "STATUS_CODE": 101}
	record := BuildAccessLogFluentRecord(&AccessLogFluentMiddleware{Prefix: "x"}, r, env, 0, nil, nil)

	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), token) || strings.Contains(string(b), ticket) {
		t.Errorf("record holds the credential: %s", b)
	}
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if _, ok := record.RequestHeaders[name]; ok {
			t.Errorf("record holds %s", name)
		}
	}
	want := []string{"chat, bearer.[redacted]", "bearer.[redacted]", "ticket.[redacted], chat", "ticket.[redacted]"}
	if got := record.RequestHeaders["Sec-Websocket-Protocol"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Sec-Websocket-Protocol %q, want %q", got, want)
	}
	if got := record.RequestHeaders["User-Agent"]; !reflect.DeepEqual(got, []string{"test/1"}) {
		t.Errorf("User-Agent %q", got)
	}
}
