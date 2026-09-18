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
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/labstack/echo/v5"
)

// Differential tests: access log records must match go-json-rest's (minus timing fields).

type jsonRec map[string]interface{}

func decodeRec(t *testing.T, buf *bytes.Buffer) jsonRec {
	t.Helper()
	var r jsonRec
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &r); err != nil {
		t.Fatalf("access log line is not JSON: %q: %v", buf.String(), err)
	}
	for _, volatile := range []string{"Timestamp", "ResponseTime"} {
		if _, ok := r[volatile]; !ok {
			t.Errorf("record lacks %s: %v", volatile, r)
		}
		delete(r, volatile)
	}
	return r
}

func logViaEcho(t *testing.T, method, path, token string) jsonRec {
	t.Helper()
	var buf bytes.Buffer
	e := New()
	e.Pre(AccessLogJSON(log.New(&buf, "", 0), "/svc"), Instrument(), Recover(), JWT(newJWT()))
	e.GET("/svc/", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, map[string]int{"n": 1}) })
	e.GET("/svc/boom", func(c *echo.Context) error { panic("x") })

	mux := http.NewServeMux()
	mux.Handle("/svc/", e)

	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("User-Agent", "differential-test/1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	mux.ServeHTTP(httptest.NewRecorder(), req)
	return decodeRec(t, &buf)
}

func TestAccessLogJSONMatchesGoJSONRest(t *testing.T) {
	token := signed(t, map[string]interface{}{"id": "user1", "exp": float64(4102444800)}, testKey)

	for _, tc := range []struct{ name, method, path, token string }{
		{"authenticated success", "GET", "/svc/?page=2", token},
		// These three are the cases a naive echo recorder logs as status 0.
		{"unauthenticated 401", "GET", "/svc/", ""},
		{"unknown route 404", "GET", "/svc/nope", token},
		{"wrong method 405", "DELETE", "/svc/", token},
		{"handler panic 500", "GET", "/svc/boom", token},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gjr := recorded[jsonRec](t, "gjr", nil)
			ech := logViaEcho(t, tc.method, tc.path, tc.token)
			if !reflect.DeepEqual(gjr, ech) {
				t.Errorf("access log record differs:\n  go-json-rest=%v\n  echo        =%v", gjr, ech)
			}
		})
	}
}
