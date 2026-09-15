// Package mongotest starts a throwaway MongoDB for tests.
//
// It exists because the Mongo-backed tests in this repo were effectively
// unrunnable: they connect via utils.GetMongoClient(), which reads MONGO_HOST /
// MONGO_PORT / MONGO_RS from the environment and defaults to localhost:27017.
// The docker-compose replica set publishes no host port, so that default
// refuses the connection and several tests then nil-deref on the failed client
// rather than skipping. The result is a suite nobody can run locally and CI
// never ran at all.
//
// A container started here is addressed by a published random port, so it needs
// no compose file, no fixed port, and nothing already running. It is also a
// real single-node replica set: utils.GetMongoClient() applies majority read
// and write concern (see utils/db.go), which a standalone mongod rejects, so a
// plain `mongo:` service image is not a substitute.
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

// Image is the MongoDB image used for tests. Keep it aligned with the server
// version deployed to stage and production; a test suite passing against a
// different major version proves less than it appears to.
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

// SetupEnv starts a MongoDB and points this process's Mongo environment at it,
// so that existing callers of utils.GetMongoClient and utils.GetMongoClientTest
// reach the container without being modified. It returns a cleanup function.
//
// Intended for TestMain:
//
//	func TestMain(m *testing.M) {
//		cleanup, code := mongotest.SetupEnv()
//		if cleanup != nil {
//			defer cleanup()
//		}
//		if code != 0 {
//			os.Exit(code)
//		}
//		os.Exit(m.Run())
//	}
//
// If PANTAHUB_TEST_MONGO_EXTERNAL is set, no container is started and whatever
// MONGO_* values are already in the environment are used unchanged -- for
// pointing a run at an existing replica set, or at a CI `services:` entry.
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

// Setup is the *testing.T flavour of SetupEnv, for a single test or subtree
// that wants its own database. It skips rather than fails when no container
// runtime is reachable, so a developer without Docker still gets a green run
// on the tests that do not need Mongo.
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
