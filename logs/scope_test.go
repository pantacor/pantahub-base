package logs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

func TestStoredLogRoutesEnforceReadScopes(t *testing.T) {
	withoutFluent(t)
	app := testApp(&stubBackend{entries: []*Entry{{LogText: "private output"}}})
	app.jwtConfig.Realm = "test"
	server := echoutil.NewServer("logs-scope-test")
	app.Mount(server)

	for _, tc := range []struct {
		name  string
		scope utils.Scope
		allow bool
	}{
		{"unrelated profile scope", utils.Scopes.ReadProfile, false},
		{"read-only API", utils.Scopes.APIReadOnly, true},
		{"read devices", utils.Scopes.ReadDevices, true},
		{"device logs", utils.Scopes.DeviceLogs, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwtgo.MapClaims{
				"type": "USER", "prn": "prn:::accounts:/testowner",
				"scopes": tc.scope.String(), "exp": time.Now().Add(time.Hour).Unix(),
			}
			token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claims).SignedString(app.jwtConfig.Key)
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range []struct{ method, path string }{
				{http.MethodGet, "/logs/"},
				{http.MethodGet, "/logs/cursor"},
				{http.MethodPost, "/logs/cursor"},
			} {
				req := httptest.NewRequest(route.method, route.path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				server.E.ServeHTTP(rec, req)
				if !tc.allow {
					if rec.Code != http.StatusForbidden {
						t.Errorf("%s %s: got %d, want 403", route.method, route.path, rec.Code)
					}
					continue
				}
				if route.path == "/logs/" {
					if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "private output") {
						t.Errorf("%s %s: got %d %s", route.method, route.path, rec.Code, rec.Body.String())
					}
				} else if rec.Code != http.StatusBadRequest { // reached the handler; cursor was omitted
					t.Errorf("%s %s: got %d, want 400", route.method, route.path, rec.Code)
				}
			}
		})
	}
}
