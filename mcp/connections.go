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
	"sync"
	"time"

	"gitlab.com/pantacor/pantahub-base/auth/storage"
)

const (
	// claimConnectionID is the claim of a bound access token that names the
	// connection it was issued under (auth.ClaimConnectionID; repeated here
	// because auth imports far more than this package wants to).
	claimConnectionID = "cnx"

	// connectionCacheTTL is how long a connection found standing is believed
	// to still stand. It is the longest an ended connection can keep working,
	// and what keeps a chatty client from costing a database read per call.
	connectionCacheTTL = 15 * time.Second

	maxCachedConnections = 4096
)

// connectionChecker answers whether the connection an access token was issued
// under still stands.
type connectionChecker struct {
	// lookup asks the authorization server's store. It is a field so tests
	// can stand in for the database.
	lookup func(ctx context.Context, connectionID string) (bool, error)

	mu       sync.Mutex
	standing map[string]time.Time
}

func newConnectionChecker() *connectionChecker {
	return &connectionChecker{
		lookup: func(ctx context.Context, connectionID string) (bool, error) {
			repo, err := storage.GetRefreshTokenRepo()
			if err != nil {
				return false, err
			}
			return repo.FamilyActive(ctx, connectionID)
		},
		standing: map[string]time.Time{},
	}
}

// active reports whether connectionID still stands. Only "yes" is remembered:
// an ended connection stays ended, so there is nothing to gain from caching
// "no", and a lookup that failed must be tried again.
func (c *connectionChecker) active(ctx context.Context, connectionID string) (bool, error) {
	now := time.Now()

	c.mu.Lock()
	until, cached := c.standing[connectionID]
	c.mu.Unlock()
	if cached && now.Before(until) {
		return true, nil
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	active, err := c.lookup(ctx, connectionID)
	if err != nil {
		return false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !active {
		delete(c.standing, connectionID)
		return false, nil
	}
	if len(c.standing) >= maxCachedConnections {
		c.standing = map[string]time.Time{}
	}
	c.standing[connectionID] = now.Add(connectionCacheTTL)
	return true, nil
}
