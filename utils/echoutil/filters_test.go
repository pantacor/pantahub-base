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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
)

// gjrErrError is the JSON-friendly form of a recorded go-json-rest-side error,
// since error values do not round-trip through the golden file.
type gjrErrError struct {
	IsNil bool
	Msg   string
}

func TestDecodeJsonPayloadMatchesFork(t *testing.T) {
	for i, body := range []string{"", `{"a":1}`, `{bad json`, `[1,2]`} {
		gErr := recorded[gjrErrError](t, fmt.Sprintf("gjr-%d", i), nil)
		var eErr error
		var ev map[string]interface{}

		e := echo.New()
		c := e.NewContext(httptest.NewRequest("POST", "/", strings.NewReader(body)), httptest.NewRecorder())
		eErr = DecodeJsonPayload(c, &ev)

		if gErr.IsNil != (eErr == nil) || (!gErr.IsNil && gErr.Msg != eErr.Error()) {
			t.Errorf("body %q: fork err=%v echo err=%v", body, gErr, eErr)
		}
		if body == "" && eErr != ErrJsonPayloadEmpty {
			t.Errorf("empty body must return ErrJsonPayloadEmpty, got %v", eErr)
		}
	}
}

func TestUserTypeFilterMatchesGoJSONRest(t *testing.T) {
	only := []accounts.AccountType{accounts.AccountTypeUser}
	for _, ctype := range []string{"USER", "DEVICE", "SERVICE"} {
		t.Run(ctype, func(t *testing.T) {
			tok := signed(t, map[string]interface{}{"id": "x", "prn": "prn:::x", "type": ctype, "exp": float64(4102444800)}, testKey)

			e := New()
			e.GET("/", func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, "ok") }, JWT(newJWT()), Auth(), UserTypeFilterMW(only))

			run := func(h http.Handler) result {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("Authorization", "Bearer "+tok)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())}
			}
			gjr := recorded[result](t, "gjr", nil)
			requireIdentical(t, gjr, run(e))
		})
	}
}
