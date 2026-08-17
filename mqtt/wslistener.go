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
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// wsSubprotocol is the only WebSocket subprotocol an MQTT client may negotiate.
// [MQTT-6.0.0-3]
const wsSubprotocol = "mqtt"

// wsCloseGraceTimeout bounds how long a close frame may take to reach a device
// that has already gone away. The socket is torn down regardless afterwards.
const wsCloseGraceTimeout = time.Second

// wsConnectTimeout bounds how long a freshly upgraded connection may take to
// send its MQTT CONNECT. The broker only sets its own read deadline once CONNECT
// has been read, so without this a peer that completes the WebSocket handshake
// and then goes silent would pin a goroutine and a socket forever. mochi resets
// the deadline from the negotiated keepalive as soon as CONNECT arrives, so this
// only ever bounds the pre-CONNECT window.
const wsConnectTimeout = 20 * time.Second

// errNotBinaryMessage is returned when a peer sends a non-binary frame. MQTT
// over WebSocket is defined over binary messages only. [MQTT-6.0.0-2]
var errNotBinaryMessage = errors.New("mqtt: websocket message type not binary")

var (
	_ listeners.Listener = (*wsListener)(nil)
	_ http.Handler       = (*wsListener)(nil)
)

// wsListener is an MQTT listener that takes its connections from an existing
// http.ServeMux instead of owning a network socket.
//
// The deployed Kubernetes service only publishes the API's HTTP ports and the
// CI pipeline only swaps the container image, so the broker cannot bind a port
// of its own. Mounting this listener as an http.Handler lets devices reach the
// broker through the API's address, port and TLS certificate.
type wsListener struct {
	id   string
	path string

	upgrader *websocket.Upgrader

	// mu guards the fields below, which are written by Init, Serve and Close
	// while ServeHTTP reads them from arbitrary request goroutines.
	mu        sync.RWMutex
	log       *slog.Logger
	establish listeners.EstablishFn

	closed    atomic.Bool
	closeOnce sync.Once
}

// newWSListener returns a listener identified by id and advertised as mounted
// at path. Routing is the mux's job; path is informational and is what
// Address reports.
func newWSListener(id, path string) *wsListener {
	return &wsListener{
		id:   id,
		path: path,
		upgrader: &websocket.Upgrader{
			Subprotocols: []string{wsSubprotocol},
			CheckOrigin:  checkWSOrigin,
		},
	}
}

// ID returns the id of the listener.
func (l *wsListener) ID() string {
	return l.id
}

// Address returns the path the listener is mounted under.
func (l *wsListener) Address() string {
	return l.path
}

// Protocol returns the transport name. It is always "ws": TLS is terminated by
// the HTTP server this listener is mounted on, never by the listener itself.
func (l *wsListener) Protocol() string {
	return "ws"
}

// Init records the broker's logger. There is no socket to open; connections
// arrive through ServeHTTP.
func (l *wsListener) Init(log *slog.Logger) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log = log
	return nil
}

// Serve records the callback used to hand established connections to the
// broker. It returns immediately: this listener does not own an accept loop,
// and Service.Start relies on the broker's Serve call returning.
func (l *wsListener) Serve(establish listeners.EstablishFn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.establish = establish
}

// Close stops accepting connections and disconnects every client of this
// listener. It is safe to call more than once.
func (l *wsListener) Close(closeClients listeners.CloseFn) {
	l.closeOnce.Do(func() {
		l.closed.Store(true)
		closeClients(l.id)
	})
}

// ServeHTTP upgrades an incoming request to a WebSocket and hands the
// connection to the broker. It blocks for the lifetime of the connection,
// which is what keeps the hijacked socket owned by this goroutine.
func (l *wsListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if l.closed.Load() {
		http.Error(w, "mqtt broker is shutting down", http.StatusServiceUnavailable)
		return
	}

	l.mu.RLock()
	establish, log := l.establish, l.log
	l.mu.RUnlock()

	// The broker calls Serve before any request can be routed here, but a mux
	// mounted ahead of Service.Start would otherwise dereference a nil
	// callback.
	if establish == nil {
		http.Error(w, "mqtt broker is not accepting connections yet", http.StatusServiceUnavailable)
		return
	}

	// Rejected before the upgrade so the client sees a diagnosable status
	// rather than a socket that closes on its first packet.
	if !offersSubprotocol(r, wsSubprotocol) {
		http.Error(w, `websocket subprotocol "mqtt" is required`, http.StatusBadRequest)
		return
	}

	conn, err := l.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written an error response.
		return
	}

	adapter := &wsConn{Conn: conn.UnderlyingConn(), ws: conn}
	defer adapter.Close()

	// Bound the pre-CONNECT window so a silent peer cannot pin this goroutine
	// and its socket indefinitely. The broker resets the deadline once it has
	// read CONNECT, so this only governs the handshake-to-CONNECT gap.
	_ = adapter.SetReadDeadline(time.Now().Add(wsConnectTimeout))

	if err := establish(l.id, adapter); err != nil && log != nil {
		log.Warn("mqtt: websocket client ended", "error", err, "listener", l.id)
	}
}

// checkWSOrigin decides whether a handshake may proceed.
//
// Devices are not browsers: they send no Origin header at all, so a missing
// Origin must be allowed or no device could connect. A present Origin means a
// browser is driving the handshake, and browsers attach ambient credentials to
// cross-site WebSocket requests; those are held to same-origin so that a
// hostile page cannot open a broker session on a visitor's behalf. Blanket
// allowing every origin would give that page a broker connection for free.
func checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host, _, _ = strings.Cut(forwarded, ",")
		host = strings.TrimSpace(host)
	}

	scheme, authority, found := strings.Cut(origin, "://")
	if !found || (scheme != "http" && scheme != "https") {
		return false
	}

	return strings.EqualFold(authority, host)
}

// offersSubprotocol reports whether the handshake advertises the given
// subprotocol in Sec-WebSocket-Protocol.
func offersSubprotocol(r *http.Request, want string) bool {
	for _, offered := range websocket.Subprotocols(r) {
		if strings.EqualFold(offered, want) {
			return true
		}
	}
	return false
}

// wsConn adapts a WebSocket connection to net.Conn, which is what the broker
// consumes. Addresses and deadlines are delegated to the embedded hijacked
// connection: the WebSocket reads and writes the very same socket, so a
// deadline set there is the deadline the broker's keepalive intends.
type wsConn struct {
	net.Conn
	ws *websocket.Conn

	// reader over the message currently being drained; nil between messages.
	// Only touched by Read, which the broker calls from a single goroutine.
	reader io.Reader

	closeOnce sync.Once
	closeErr  error
}

// Read fills p from the current WebSocket message, moving on to the next
// message once the current one is exhausted. A message larger than p is
// drained across successive calls, so the broker sees a byte stream and never
// a message boundary. Like any net.Conn, Read blocks until it has at least one
// byte or an error, and so never reports a zero-length read.
func (c *wsConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	var n int
	for {
		if c.reader == nil {
			messageType, reader, err := c.ws.NextReader()
			if err != nil {
				return n, err
			}

			if messageType != websocket.BinaryMessage {
				return n, errNotBinaryMessage
			}

			c.reader = reader
		}

		for n < len(p) {
			read, err := c.reader.Read(p[n:])
			n += read
			if err != nil {
				// Any error ends the current message: io.EOF because it was
				// fully read, anything else because the remainder is not
				// trustworthy.
				c.reader = nil
				if !errors.Is(err, io.EOF) {
					return n, err
				}
				break
			}
		}

		// Either p is full or the message ran out. Only an empty message
		// leaves nothing to report, in which case wait for the next one.
		if n > 0 || c.reader != nil {
			return n, nil
		}
	}
}

// Write sends p as exactly one binary WebSocket message. The broker serializes
// its writes, so no locking is needed here.
func (c *wsConn) Write(p []byte) (int, error) {
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close tears the connection down, best-effort announcing it with a WebSocket
// close frame first. It is idempotent: the broker closes connections from both
// its client goroutine and its shutdown path.
func (c *wsConn) Close() error {
	c.closeOnce.Do(func() {
		// WriteControl is safe to call concurrently with an in-flight Write.
		_ = c.ws.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(wsCloseGraceTimeout),
		)
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}
