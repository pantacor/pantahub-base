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
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v5"
)

// KeyWebSocketProtocol holds the WebSocket subprotocol WebSocketTicket took
// the ticket from, which the handler must echo back (and nothing else) when it
// upgrades: a browser drops a handshake whose answer names no protocol it
// offered. It lives in the request context only: the request header that
// carried it is rewritten, so that nothing logging request headers later
// records the ticket.
const KeyWebSocketProtocol = "PH_WEBSOCKET_PROTOCOL"

// KeyWebSocketTicket holds the ticket offered as the subprotocol, for the
// handler to consume: a ticket is not a JWT, so the JWT middleware cannot
// authenticate the request; the handler that issued the ticket does.
const KeyWebSocketTicket = "PH_WEBSOCKET_TICKET"

// WebSocketTicketPrefix marks the subprotocol that carries a session ticket.
const WebSocketTicketPrefix = "ticket."

// WebSocketBearerPrefix marks the subprotocol that used to carry a bearer
// token. The 101 response echoes the chosen subprotocol, where proxies and
// HAR files record it, so a long-lived token must never be offered: the
// offer is refused.
const WebSocketBearerPrefix = "bearer."

// WebSocketChallenge is the WWW-Authenticate of a refused WebSocket
// handshake: never Basic, so that a browser does not prompt for credentials
// it would then attach to every later request.
const WebSocketChallenge = `Bearer realm="pantahub"`

// webSocketTicketPattern is a ticket as issued: 32 random bytes in base64url
// without padding.
var webSocketTicketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// WebSocketTicket takes the session ticket out of a WebSocket handshake, and
// admits nothing else as a credential: browsers cannot set headers on a
// WebSocket, so the ticket travels as the subprotocol "ticket.<ticket>".
//
// On a WebSocket upgrade, the first well-formed such subprotocol is recorded
// under KeyWebSocketTicket, and the offer itself under KeyWebSocketProtocol,
// for the handler that consumes the ticket. The ticket offers are then
// removed from the request's Sec-WebSocket-Protocol header (any other offer
// stays), so the ticket is never in the request headers an access log
// records.
//
// Ambient and long-lived credentials are refused: a request that carries an
// Authorization header (Basic with a personal token cached by the browser, a
// bearer token, or anything else) is answered 401, as is a "bearer.<token>"
// offer, and the Cookie header is dropped. A request without a ticket goes on
// with none in the context, for the handler to refuse.
//
// Mount it only on the routes that serve WebSockets, and keep the JWT
// middleware off them: the ticket carries the caller's authority.
func WebSocketTicket() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			r := c.Request()
			r.Header.Del("Cookie")
			if r.Header.Get("Authorization") != "" {
				return WebSocketUnauthorized(c, "WebSocket takes its ticket from the subprotocol only")
			}
			if !isWebSocketUpgrade(r) {
				return next(c)
			}
			protocol, ticket, others, bearer := ticketSubprotocol(r)
			if len(others) > 0 {
				r.Header.Set("Sec-WebSocket-Protocol", strings.Join(others, ", "))
			} else {
				r.Header.Del("Sec-WebSocket-Protocol")
			}
			if bearer {
				return WebSocketUnauthorized(c, "WebSocket takes a session ticket, not a bearer token")
			}
			if ticket != "" {
				c.Set(KeyWebSocketTicket, ticket)
				c.Set(KeyWebSocketProtocol, protocol)
			}
			return next(c)
		}
	}
}

// WebSocketUnauthorized refuses a WebSocket handshake with 401 and the
// challenge a browser does not turn into a credentials prompt.
func WebSocketUnauthorized(c *echo.Context, message string) error {
	c.Response().Header().Set("WWW-Authenticate", WebSocketChallenge)
	return Error(c, message, http.StatusUnauthorized)
}

// ticketSubprotocol returns the first well-formed "ticket.<ticket>"
// subprotocol offered, the offers that are neither tickets nor bearer tokens,
// and whether a "bearer.<token>" was offered. A malformed ticket offer is
// dropped with the rest, never kept in the header.
func ticketSubprotocol(r *http.Request) (protocol, ticket string, others []string, bearer bool) {
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, offered := range strings.Split(header, ",") {
			offered = strings.TrimSpace(offered)
			if offered == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(offered), WebSocketBearerPrefix) {
				bearer = true
				continue
			}
			value, found := strings.CutPrefix(offered, WebSocketTicketPrefix)
			if !found {
				others = append(others, offered)
				continue
			}
			if ticket == "" && webSocketTicketPattern.MatchString(value) {
				protocol, ticket = offered, value
			}
		}
	}
	return protocol, ticket, others, bearer
}

// isWebSocketUpgrade reports whether r asks for a WebSocket.
func isWebSocketUpgrade(r *http.Request) bool {
	return headerHasToken(r, "Connection", "upgrade") && headerHasToken(r, "Upgrade", "websocket")
}

func headerHasToken(r *http.Request, name, token string) bool {
	for _, header := range r.Header.Values(name) {
		for _, value := range strings.Split(header, ",") {
			if strings.EqualFold(strings.TrimSpace(value), token) {
				return true
			}
		}
	}
	return false
}
