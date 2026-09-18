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
	"fmt"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"
)

// Server is the global echo instance services migrate onto. Each service's
// middleware chain runs before routing, as in go-json-rest, so e.g. an
// unauthenticated request to an unknown route still answers 401.
type Server struct {
	E        *echo.Echo
	services []mount
}

type mount struct {
	prefix string // e.g. "/dash", no trailing slash
	chain  []echo.MiddlewareFunc
}

// NewServer returns the global server with go-json-rest-compatible routing
// (see New) and tracing under serviceName.
func NewServer(serviceName string) *Server {
	s := &Server{E: New()}
	s.E.Use(Otel(serviceName))
	s.E.Pre(s.dispatch)
	return s
}

// Mount registers a service chain for prefix (e.g. "/dash") and returns a group
// for its full-path routes.
func (s *Server) Mount(prefix string, chain ...echo.MiddlewareFunc) *echo.Group {
	s.services = append(s.services, mount{prefix: prefix, chain: chain})
	// Longest prefix first, so a nested mount (if one is ever added) wins
	// over its parent.
	sort.SliceStable(s.services, func(i, j int) bool {
		return len(s.services[i].prefix) > len(s.services[j].prefix)
	})
	return s.E.Group(prefix)
}

// Validate rejects routes still in go-json-rest syntax: echo treats "#id" as
// literal text, so such a route would silently 404.
func (s *Server) Validate() error {
	for _, r := range s.E.Router().Routes() {
		if strings.Contains(r.Path, "#") {
			return fmt.Errorf("echo route %s %s uses go-json-rest #param syntax; use :param", r.Method, r.Path)
		}
	}
	return nil
}

// dispatch wraps routing in the chain of the service owning the request path.
func (s *Server) dispatch(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		path := c.Request().URL.Path
		for _, m := range s.services {
			if path == m.prefix || strings.HasPrefix(path, m.prefix+"/") {
				h := next
				for i := len(m.chain) - 1; i >= 0; i-- {
					h = m.chain[i](h)
				}
				return h(c)
			}
		}
		// The mux only forwards prefixes that were mounted, so this is not
		// expected; route without any service chain rather than guess one.
		return next(c)
	}
}
