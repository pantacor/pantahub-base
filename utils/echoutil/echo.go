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
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/labstack/echo/v5"
)

// New returns echo with go-json-rest-compatible routing: {"Error":...} 404/405
// bodies, no Allow header, 405 instead of OPTIONS->204, and no :param spanning "/".
func New() *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = HTTPErrorHandler
	e.Use(routerCompat)
	return e
}

// routerCompat corrects the two cases where echo's router resolves a request
// differently from go-json-rest's. It runs after routing.
func routerCompat(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// echo lets a trailing :param swallow segments (/x/:id matches /x/a/b);
		// go-json-rest's #param stops at "/". Treat as not found.
		for _, v := range c.PathValues() {
			if strings.Contains(v.Value, "/") {
				return routingError(c)
			}
		}

		// Set only when the path matched but the method did not (405 or OPTIONS).
		if _, pathMatchedMethodDidNot := c.Get(echo.ContextKeyHeaderAllow).(string); pathMatchedMethodDidNot {
			return routingError(c)
		}

		return next(c)
	}
}

// routingError answers an unrouted request as go-json-rest does: 405 when any
// route's path matches (whatever its method), else 404. echo's own choice
// differs when routes overlap, e.g. /tokens/:id vs /:id/ownership/validate.
func routingError(c *echo.Context) error {
	c.Response().Header().Del(echo.HeaderAllow)
	path := strings.Split(c.Request().URL.EscapedPath(), "/")
	for _, r := range c.Echo().Router().Routes() {
		if pathMatches(strings.Split(r.Path, "/"), path) {
			return Error(c, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
	return NotFound(c)
}

// pathMatches reports whether a route path matches the escaped request path,
// a :param matching one non-empty segment like go-json-rest's #param.
func pathMatches(route, path []string) bool {
	if len(route) != len(path) {
		return false
	}
	for i, seg := range route {
		if strings.HasPrefix(seg, ":") {
			if path[i] == "" {
				return false
			}
		} else if seg != path[i] {
			return false
		}
	}
	return true
}

// HTTPErrorHandler answers routing errors in go-json-rest's shape and anything
// else as a generic 500, never exposing err.Error().
func HTTPErrorHandler(c *echo.Context, err error) {
	if Committed(c) {
		return
	}

	// v5's routing sentinels are an unexported type, so ask for the status code
	// rather than type-asserting *echo.HTTPError.
	switch echo.StatusCode(err) {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		_ = routingError(c)
		return
	}

	log.Printf("ERROR: unhandled error from echo handler %s %s: %v", c.Request().Method, c.Request().URL.Path, err)
	_ = Error(c, "Internal Server Error", http.StatusInternalServerError)
}

// Recover mirrors rest.RecoverMiddleware: trace to stderr, generic 500 to client.
func Recover() echo.MiddlewareFunc {
	logger := log.New(os.Stderr, "", 0)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) (err error) {
			defer func() {
				if reco := recover(); reco != nil {
					logger.Print(fmt.Sprintf("%s\n%s", reco, debug.Stack()))
					if !Committed(c) {
						err = Error(c, "Internal Server Error", http.StatusInternalServerError)
					}
				}
			}()
			return next(c)
		}
	}
}

// Response unwraps echo's *Response: v5's Context.Response() hands back the bare
// http.ResponseWriter, but Status/Size/Committed live on the wrapper. nil only if
// something replaced the writer outside echo's serve chain.
func Response(c *echo.Context) *echo.Response {
	res, err := echo.UnwrapResponse(c.Response())
	if err != nil {
		return nil
	}
	return res
}

// Committed reports whether the response has been written.
func Committed(c *echo.Context) bool {
	res := Response(c)
	return res != nil && res.Committed
}
