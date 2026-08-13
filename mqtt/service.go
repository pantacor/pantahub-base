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

package mqtt

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

// Environment variables. Defaults are chosen so that a deployment which only
// swaps the container image — which is how this service reaches staging — comes
// up with the message plane available over the existing HTTP port.
const (
	// EnvMqttEnabled turns the whole message plane on or off.
	EnvMqttEnabled = "PANTAHUB_MQTT_ENABLED"

	// EnvMqttWsPath is where the WebSocket transport is mounted on the main
	// HTTP mux. Devices connect to wss://<host><path>.
	EnvMqttWsPath = "PANTAHUB_MQTT_WS_PATH"

	// EnvMqttTCPAddress optionally opens a native MQTT TCP listener. Empty
	// disables it; the shipped Kubernetes service only publishes the HTTP
	// ports, so this is for local development and future native ingress.
	EnvMqttTCPAddress = "PANTAHUB_MQTT_TCP_ADDRESS"

	// EnvMqttSessionExpiry is how long, in seconds, the broker keeps a
	// disconnected device's session so queued messages survive a reboot or a
	// dead uplink.
	EnvMqttSessionExpiry = "PANTAHUB_MQTT_SESSION_EXPIRY_SECONDS"
)

const (
	defaultWsPath = "/mqtt/"

	// A week of session retention: long enough that a device which loses
	// connectivity over a weekend still finds its queued work on return.
	defaultSessionExpiry = 7 * 24 * time.Hour

	// Log batches are the largest thing a device sends; keep headroom over
	// the agent's 256 KB batch cap without inviting unbounded payloads.
	maxPacketSize = 512 * 1024
)

// Collections the message plane reads, writes and watches. These are the same
// collections the REST handlers use, so a device reporting over MQTT lands in
// exactly the state a polling device would have produced.
const (
	devicesCollection = "pantahub_devices"
	stepsCollection   = "pantahub_steps"
	trailsCollection  = "pantahub_trails"
)

// Service owns the embedded MQTT broker and the goroutines that bridge it to
// the rest of Pantahub.
type Service struct {
	server      *mochi.Server
	mongoClient *mongo.Client
	ws          *wsListener
	notifier    *Notifier
	tcpAddress  string
	wsPath      string
}

// Enabled reports whether the message plane should be started at all.
func Enabled() bool {
	enabled, err := strconv.ParseBool(utils.GetEnvDefault(EnvMqttEnabled, "true"))
	if err != nil {
		return false
	}
	return enabled
}

// WsPath is the path the WebSocket transport is mounted under. It always ends
// in a slash so it can be registered as a subtree on http.ServeMux.
func WsPath() string {
	path := utils.GetEnvDefault(EnvMqttWsPath, defaultWsPath)
	if path == "" {
		return defaultWsPath
	}
	if path[len(path)-1] != '/' {
		path += "/"
	}
	return path
}

func sessionExpiry() time.Duration {
	raw := utils.GetEnvDefault(EnvMqttSessionExpiry, "")
	if raw == "" {
		return defaultSessionExpiry
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultSessionExpiry
	}

	return time.Duration(seconds) * time.Second
}

// New builds the broker, installs the authentication and bridge hooks, and
// mounts the WebSocket transport. Nothing is served until Start is called.
func New(mongoClient *mongo.Client) (*Service, error) {
	expiry := sessionExpiry()

	capabilities := mochi.NewDefaultServerCapabilities()
	capabilities.MaximumPacketSize = maxPacketSize
	capabilities.MaximumSessionExpiryInterval = uint32(expiry.Seconds())

	server := mochi.New(&mochi.Options{
		Capabilities:           capabilities,
		InlineClient:           true,
		SysTopicResendInterval: int64(time.Minute.Seconds()),
		Logger:                 slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	service := &Service{
		server:      server,
		mongoClient: mongoClient,
		tcpAddress:  utils.GetEnvDefault(EnvMqttTCPAddress, ""),
		wsPath:      WsPath(),
	}

	if err := server.AddHook(&authHook{mongoClient: mongoClient}, nil); err != nil {
		return nil, err
	}

	if err := server.AddHook(&bridgeHook{mongoClient: mongoClient}, nil); err != nil {
		return nil, err
	}

	service.ws = newWSListener("pantahub-ws", service.wsPath)
	if err := server.AddListener(service.ws); err != nil {
		return nil, err
	}

	if service.tcpAddress != "" {
		tcp := listeners.NewTCP(listeners.Config{
			ID:      "pantahub-tcp",
			Address: service.tcpAddress,
		})
		if err := server.AddListener(tcp); err != nil {
			return nil, err
		}
	}

	service.notifier = NewNotifier(mongoClient, server)

	return service, nil
}

// Handler returns the WebSocket transport as an http.Handler so it can be
// mounted on the main API mux. Devices reach the broker through the same
// origin, port and TLS certificate as the REST API.
func (s *Service) Handler() http.Handler {
	return s.ws
}

// Start begins serving connections and starts watching for changes that must
// be pushed to devices. It does not block.
func (s *Service) Start(ctx context.Context) error {
	if err := s.server.Serve(); err != nil {
		return err
	}

	go func() {
		if err := s.notifier.Run(ctx); err != nil && ctx.Err() == nil {
			log.Println("mqtt: notifier stopped: " + err.Error())
		}
	}()

	return nil
}

// Close shuts the broker down and disconnects every client.
func (s *Service) Close() error {
	return s.server.Close()
}
