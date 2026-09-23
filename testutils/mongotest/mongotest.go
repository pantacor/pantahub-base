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

// Package mongotest starts a throwaway MongoDB replica set for tests. A replica
// set is required: utils/db.go uses majority read/write concern.
package mongotest

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
)

// Image should track the server version deployed to stage/prod.
const Image = "mongo:6.0"

// startTimeout bounds container pull plus replica-set election.
const startTimeout = 3 * time.Minute

// Container is a running throwaway MongoDB.
type Container struct {
	URI       string
	Host      string
	Port      string
	terminate func(context.Context) error
}

// Start launches a single-node replica set and returns it. The caller is
// responsible for Stop; prefer SetupEnv, which wires teardown automatically.
func Start(ctx context.Context) (*Container, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	ctr, err := mongodb.Run(ctx, Image, mongodb.WithReplicaSet("rs0"))
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", Image, err)
	}

	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("resolving connection string: %w", err)
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("parsing connection string %q: %w", uri, err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("splitting host/port from %q: %w", parsed.Host, err)
	}

	return &Container{
		URI:  uri,
		Host: host,
		Port: port,
		terminate: func(c context.Context) error {
			return testcontainers.TerminateContainer(ctr)
		},
	}, nil
}

// Stop removes the container.
func (c *Container) Stop(ctx context.Context) error {
	if c == nil || c.terminate == nil {
		return nil
	}
	return c.terminate(ctx)
}

// SetupEnv starts MongoDB and points MONGO_* at it, for TestMain. Set
// PANTAHUB_TEST_MONGO_EXTERNAL to use the existing environment instead.
func SetupEnv() (cleanup func(), exitCode int) {
	if os.Getenv("PANTAHUB_TEST_MONGO_EXTERNAL") != "" {
		return func() {}, 0
	}

	ctx := context.Background()
	ctr, err := Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mongotest: %v\n", err)
		// 0 here would let the suite run on and fail with a confusing
		// connection error. Report it as an infrastructure failure instead.
		return nil, 1
	}

	restore := setEnv(map[string]string{
		"MONGO_HOST": ctr.Host,
		"MONGO_PORT": ctr.Port,
		"MONGO_RS":   "rs0",
		"MONGO_USER": "",
		"MONGO_PASS": "",
		"MONGO_SSL":  "",
	})

	return func() {
		restore()
		if err := ctr.Stop(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "mongotest: terminating container: %v\n", err)
		}
	}, 0
}

// Setup is SetupEnv for a single test; it skips if no container runtime.
func Setup(t *testing.T) *Container {
	t.Helper()

	ctr, err := Start(context.Background())
	if err != nil {
		t.Skipf("mongotest: no container runtime available (%v)", err)
		return nil
	}

	restore := setEnv(map[string]string{
		"MONGO_HOST": ctr.Host,
		"MONGO_PORT": ctr.Port,
		"MONGO_RS":   "rs0",
		"MONGO_USER": "",
		"MONGO_PASS": "",
		"MONGO_SSL":  "",
	})
	t.Cleanup(func() {
		restore()
		if err := ctr.Stop(context.Background()); err != nil {
			t.Logf("mongotest: terminating container: %v", err)
		}
	})
	return ctr
}

// setEnv applies values and returns a function restoring the previous state,
// including unsetting variables that were not present before.
func setEnv(values map[string]string) (restore func()) {
	type prev struct {
		val string
		ok  bool
	}
	saved := make(map[string]prev, len(values))

	for k, v := range values {
		old, ok := os.LookupEnv(k)
		saved[k] = prev{val: old, ok: ok}
		if v == "" {
			_ = os.Unsetenv(k)
			continue
		}
		_ = os.Setenv(k, v)
	}

	return func() {
		for k, p := range saved {
			if p.ok {
				_ = os.Setenv(k, p.val)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}
