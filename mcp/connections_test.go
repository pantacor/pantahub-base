//
// Copyright 2026 Pantacor Ltd.
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

package mcp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ending a connection has to cut a client off even though the token it holds
// is perfectly valid and has an hour left.
func TestEndedConnectionIsCutOffAtOnce(t *testing.T) {
	service := newTestService(t, false)
	standing := map[string]bool{"cnx-1": true}
	lookups := 0
	service.connections.lookup = func(_ context.Context, id string) (bool, error) {
		lookups++
		return standing[id], nil
	}
	claims := userClaims(devicesScope)
	claims["cnx"] = "cnx-1"
	token := sign(t, testKey, claims)

	require.Equal(t, http.StatusOK, rpc(t, service, token, initializeBody).Code)
	require.Equal(t, http.StatusOK, rpc(t, service, token, listToolsBody).Code)
	assert.Equal(t, 1, lookups, "a connection found standing is not looked up on every call")

	// The user disconnects the application. The cache may hold for a few
	// seconds; past it, the same token is refused.
	standing["cnx-1"] = false
	service.connections.mu.Lock()
	service.connections.standing = map[string]time.Time{}
	service.connections.mu.Unlock()

	rec := rpc(t, service, token, listToolsBody)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata", "so the client knows how to connect again")

	// "Ended" is never cached: it is asked again, and stays refused.
	before := lookups
	assert.Equal(t, http.StatusUnauthorized, rpc(t, service, token, listToolsBody).Code)
	assert.Greater(t, lookups, before)
}

func TestConnectionLookupFailureIsNotCached(t *testing.T) {
	checker := newConnectionChecker()
	fail := true
	checker.lookup = func(context.Context, string) (bool, error) {
		if fail {
			return false, errors.New("database unavailable")
		}
		return true, nil
	}

	_, err := checker.active(context.Background(), "cnx-1")
	require.Error(t, err)

	fail = false
	active, err := checker.active(context.Background(), "cnx-1")
	require.NoError(t, err)
	assert.True(t, active, "a failed lookup must not stick")
}

func TestConnectionCacheCannotGrowWithoutBound(t *testing.T) {
	checker := newConnectionChecker()
	checker.lookup = func(context.Context, string) (bool, error) { return true, nil }
	for i := 0; i < maxCachedConnections+10; i++ {
		_, err := checker.active(context.Background(), string(rune(0x4e00+i)))
		require.NoError(t, err)
	}
	assert.LessOrEqual(t, len(checker.standing), maxCachedConnections)
}
