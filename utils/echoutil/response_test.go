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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/labstack/echo/v5"
)

// Differential tests: go-json-rest and echo must produce identical bytes.

// FLUENT_PORT defaults to 24224; empty disables fluentd so getLogger's
// log.Fatalln does not kill the test binary.
func TestMain(m *testing.M) {
	if err := os.Setenv("FLUENT_PORT", ""); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// Incident ids are minted from the clock, so two independent requests can never
// share one. Masking the digits keeps the comparison about format and shape.
var incidentID = regexp.MustCompile(`REST-ERR-ID-[0-9]+`)

func mask(b string) string { return incidentID.ReplaceAllString(b, "REST-ERR-ID-N") }

type result struct {
	Status      int
	ContentType string
	Body        string
}

func viaEcho(t *testing.T, h echo.HandlerFunc) result {
	t.Helper()
	e := echo.New()
	e.GET("/", h)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return result{rec.Code, rec.Header().Get("Content-Type"), mask(rec.Body.String())}
}

func requireIdentical(t *testing.T, gjr, ech result) {
	t.Helper()
	if gjr.Status != ech.Status {
		t.Errorf("status: go-json-rest=%d echo=%d", gjr.Status, ech.Status)
	}
	if gjr.ContentType != ech.ContentType {
		t.Errorf("Content-Type:\n  go-json-rest=%q\n  echo        =%q", gjr.ContentType, ech.ContentType)
	}
	if gjr.Body != ech.Body {
		t.Errorf("body:\n  go-json-rest=%q\n  echo        =%q", gjr.Body, ech.Body)
	}
}

func TestWriteJSONMatchesGoJSONRest(t *testing.T) {
	// HTML-significant characters and non-ASCII are where json.Marshal and a
	// differently configured encoder would first diverge.
	payload := map[string]interface{}{
		"nick":  "<device> & \"quoted\"",
		"count": 3,
		"utf8":  "ñandú",
		"nested": map[string]interface{}{
			"list": []int{1, 2, 3},
		},
	}
	requireIdentical(t,
		recorded[result](t, "gjr", nil),
		viaEcho(t, func(c *echo.Context) error { return WriteJSON(c, http.StatusOK, payload) }),
	)
}

func TestErrorMatchesRestError(t *testing.T) {
	requireIdentical(t,
		recorded[result](t, "gjr", nil),
		viaEcho(t, func(c *echo.Context) error { return Error(c, "Not Authorized", http.StatusUnauthorized) }),
	)
}

func TestNotFoundMatchesRestNotFound(t *testing.T) {
	requireIdentical(t,
		recorded[result](t, "gjr", nil),
		viaEcho(t, NotFound),
	)
}

func TestRestErrorWrapperMatches(t *testing.T) {
	// The 404 a device now receives when polling for a revision that does not
	// exist yet (trails/get-trails-rev.go).
	requireIdentical(t,
		recorded[result](t, "gjr", nil),
		viaEcho(t, func(c *echo.Context) error {
			return RestErrorWrapper(c, "No step 7 for trail abc", http.StatusNotFound)
		}),
	)
}

func TestRestErrorWrapperUserMatches(t *testing.T) {
	requireIdentical(t,
		recorded[result](t, "gjr", nil),
		viaEcho(t, func(c *echo.Context) error {
			return RestErrorWrapperUser(c, "internal detail", "Authentication Failed", http.StatusUnauthorized)
		}),
	)
}

func TestRestErrorMatchesIncludingNilError(t *testing.T) {
	for name, err := range map[string]error{"nil": nil, "non-nil": errors.New("boom")} {
		t.Run(name, func(t *testing.T) {
			requireIdentical(t,
				recorded[result](t, "gjr", nil),
				viaEcho(t, func(c *echo.Context) error {
					return RestError(c, err, "Failed", http.StatusInternalServerError)
				}),
			)
		})
	}
}

// The internal error text must never reach the client. That is the security
// property RError exists for, and the migration must not weaken it.
func TestInternalErrorTextNeverReachesClient(t *testing.T) {
	const secret = "mongo: dial tcp 10.0.0.5:27017: connection refused"
	res := viaEcho(t, func(c *echo.Context) error {
		return RestErrorWrapper(c, secret, http.StatusInternalServerError)
	})
	if regexp.MustCompile(regexp.QuoteMeta("10.0.0.5")).MatchString(res.Body) {
		t.Fatalf("internal error detail leaked to the client: %s", res.Body)
	}
}

func TestWriteHeaderNotModifiedMatches(t *testing.T) {
	requireIdentical(t,
		recorded[result](t, "gjr", nil),
		viaEcho(t, func(c *echo.Context) error { return WriteHeader(c, http.StatusNotModified) }),
	)
}
