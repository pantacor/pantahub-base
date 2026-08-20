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
	"crypto/tls"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"gitlab.com/pantacor/pantahub-base/logs"
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
	// disables it. With EnvMqttTLSCert/Key set the listener serves MQTTS
	// (MQTT over TLS, conventionally :8883); without them it is plain MQTT
	// (:1883), which must only be exposed on a trusted network.
	EnvMqttTCPAddress = "PANTAHUB_MQTT_TCP_ADDRESS"

	// EnvMqttTLSCert and EnvMqttTLSKey point at a PEM certificate and key the
	// native TCP listener terminates TLS with, so it speaks MQTTS. The ingress
	// in front of the broker passes 8883 straight through (nginx TCP services
	// do not terminate TLS), so the broker is the TLS endpoint. Both must be
	// set to enable TLS; the files are re-read on each handshake so a
	// cert-manager rotation is picked up without a restart.
	EnvMqttTLSCert = "PANTAHUB_MQTT_TLS_CERT"
	EnvMqttTLSKey  = "PANTAHUB_MQTT_TLS_KEY"

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

	// stop cancels the notifier's change-stream goroutines on Close. Without
	// it those goroutines outlive the broker, holding a Mongo cursor open until
	// the process exits.
	stop context.CancelFunc
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
//
// logsApp must be the same App the REST /logs service was built with. Its
// backend is the only registered one in the process, and registration is what
// marks an elastic backend usable; a second backend built here would look
// configured and silently discard every entry.
func New(mongoClient *mongo.Client, logsApp *logs.App) (*Service, error) {
	if logsApp == nil {
		return nil, errors.New("mqtt: no logs app")
	}

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

	if err := server.AddHook(&bridgeHook{mongoClient: mongoClient, logs: logsApp}, nil); err != nil {
		return nil, err
	}

	service.ws = newWSListener("pantahub-ws", service.wsPath)
	if err := server.AddListener(service.ws); err != nil {
		return nil, err
	}

	if service.tcpAddress != "" {
		tlsConfig, err := tcpTLSConfig()
		if err != nil {
			return nil, err
		}
		tcp := listeners.NewTCP(listeners.Config{
			ID:        "pantahub-tcp",
			Address:   service.tcpAddress,
			TLSConfig: tlsConfig,
		})
		if err := server.AddListener(tcp); err != nil {
			return nil, err
		}
	}

	service.notifier = NewNotifier(mongoClient, server)

	// Answers device pull requests (user-meta/get, steps/get) by replying
	// through the notifier, so a device seeds itself over the one MQTT socket
	// on connect. Added after the notifier exists, and after bridgeHook, whose
	// OnPublish leaves these request topics untouched.
	if err := server.AddHook(newRequestHook(service.notifier), nil); err != nil {
		return nil, err
	}

	return service, nil
}

// tcpTLSConfig builds the TLS configuration for the native listener from the
// configured cert and key, or returns nil to leave the listener as plain MQTT.
// Requiring both env vars keeps a half-configured deployment (a cert but no
// key) from silently coming up unencrypted. The certificate is resolved per
// handshake through certReloader, so a cert-manager rotation of the mounted
// secret is served without restarting the pod.
func tcpTLSConfig() (*tls.Config, error) {
	certPath := utils.GetEnvDefault(EnvMqttTLSCert, "")
	keyPath := utils.GetEnvDefault(EnvMqttTLSKey, "")
	if certPath == "" && keyPath == "" {
		return nil, nil
	}
	if certPath == "" || keyPath == "" {
		return nil, errors.New("mqtt: both " + EnvMqttTLSCert + " and " + EnvMqttTLSKey + " must be set for MQTTS")
	}

	reloader := &certReloader{certPath: certPath, keyPath: keyPath}
	// Load once up front so a missing or malformed cert fails startup loudly
	// rather than every handshake later.
	if _, err := reloader.load(); err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: reloader.GetCertificate,
	}, nil
}

// certReloader serves the TLS certificate from disk, reloading it when the
// files change. cert-manager writes a renewed certificate into the same mounted
// secret, so a broker that read the cert once at startup would keep presenting
// the old one until it restarted; reloading on modification time keeps the
// served certificate current across a rotation.
type certReloader struct {
	certPath string
	keyPath  string

	mu       sync.Mutex
	cert     *tls.Certificate
	loadedAt time.Time
}

// GetCertificate satisfies tls.Config.GetCertificate.
func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.load()
}

func (r *certReloader) load() (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reload only when the certificate file has changed since the last load,
	// so the common handshake path is a stat, not a parse.
	if r.cert != nil {
		if info, err := os.Stat(r.certPath); err == nil && !info.ModTime().After(r.loadedAt) {
			return r.cert, nil
		}
	}

	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		if r.cert != nil {
			// A rotation caught mid-write leaves the pair briefly inconsistent;
			// keep serving the last good certificate rather than failing the
			// handshake, and try again on the next one.
			log.Printf("mqtt: keeping previous TLS certificate, reload failed: %v", err)
			return r.cert, nil
		}
		return nil, err
	}

	r.cert = &cert
	r.loadedAt = time.Now()
	return r.cert, nil
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

	runCtx, cancel := context.WithCancel(ctx)
	s.stop = cancel

	go func() {
		if err := s.notifier.Run(runCtx); err != nil && runCtx.Err() == nil {
			log.Println("mqtt: notifier stopped: " + err.Error())
		}
	}()

	return nil
}

// Close shuts the broker down, cancels the notifier and disconnects every
// client. The notifier is cancelled first so its change-stream goroutines and
// their Mongo cursors are released rather than left running past shutdown.
func (s *Service) Close() error {
	if s.stop != nil {
		s.stop()
	}
	return s.server.Close()
}
