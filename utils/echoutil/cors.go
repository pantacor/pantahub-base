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
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
)

// CORSConfig configures CORS for one service.
type CORSConfig struct {
	RejectNonCorsRequests         bool
	OriginValidator               func(origin string, r *http.Request) bool
	AllowedMethods                []string
	AllowedHeaders                []string
	AccessControlExposeHeaders    []string
	AccessControlAllowCredentials bool
	AccessControlMaxAge           int
}

// AllowAllOrigins accepts any origin.
func AllowAllOrigins(string, *http.Request) bool { return true }

// CORS keeps the API's CORS behaviour: 403 on unlisted preflight headers or
// methods, 200 on preflight. Must run before routing.
func CORS(cfg CORSConfig) echo.MiddlewareFunc {
	allowedMethods := map[string]bool{}
	normedMethods := []string{}
	for _, m := range cfg.AllowedMethods {
		normed := strings.ToUpper(m)
		allowedMethods[normed] = true
		normedMethods = append(normedMethods, normed)
	}
	allowedMethodsCsv := strings.Join(normedMethods, ",")

	allowedHeaders := map[string]bool{}
	normedHeaders := []string{}
	for _, h := range cfg.AllowedHeaders {
		normed := http.CanonicalHeaderKey(h)
		allowedHeaders[normed] = true
		normedHeaders = append(normedHeaders, normed)
	}
	allowedHeadersCsv := strings.Join(normedHeaders, ",")

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			info := parseCors(c.Request())
			h := c.Response().Header()

			if !info.isCors {
				if cfg.RejectNonCorsRequests {
					return Error(c, "Non CORS request", http.StatusForbidden)
				}
				return next(c)
			}

			if !cfg.OriginValidator(info.origin, c.Request()) {
				return Error(c, "Invalid Origin", http.StatusForbidden)
			}

			if info.isPreflight {
				if !allowedMethods[info.requestMethod] {
					return Error(c, "Invalid Preflight Request", http.StatusForbidden)
				}
				for _, requested := range info.requestHeaders {
					if !allowedHeaders[requested] {
						return Error(c, "Invalid Preflight Request", http.StatusForbidden)
					}
				}
				h.Set("Access-Control-Allow-Methods", allowedMethodsCsv)
				h.Set("Access-Control-Allow-Headers", allowedHeadersCsv)
				h.Set("Access-Control-Allow-Origin", info.origin)
				if cfg.AccessControlAllowCredentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				h.Set("Access-Control-Max-Age", strconv.Itoa(cfg.AccessControlMaxAge))
				return WriteHeader(c, http.StatusOK)
			}

			for _, exposed := range cfg.AccessControlExposeHeaders {
				h.Add("Access-Control-Expose-Headers", exposed)
			}
			h.Set("Access-Control-Allow-Origin", info.origin)
			if cfg.AccessControlAllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			return next(c)
		}
	}
}

type corsInfo struct {
	isCors, isPreflight bool
	origin              string
	requestMethod       string
	requestHeaders      []string
}

// parseCors: a same-host Origin is not CORS; Origin "null" is.
func parseCors(r *http.Request) corsInfo {
	origin := r.Header.Get("Origin")
	isCors := false
	switch {
	case origin == "":
	case origin == "null":
		isCors = true
	default:
		u, err := url.ParseRequestURI(origin)
		isCors = err == nil && r.Host != u.Host
	}

	reqHeaders := []string{}
	for _, raw := range r.Header[http.CanonicalHeaderKey("Access-Control-Request-Headers")] {
		if raw == "" {
			continue
		}
		for _, h := range strings.Split(raw, ",") {
			reqHeaders = append(reqHeaders, http.CanonicalHeaderKey(strings.TrimSpace(h)))
		}
	}

	reqMethod := r.Header.Get("Access-Control-Request-Method")
	return corsInfo{
		isCors:         isCors,
		isPreflight:    isCors && r.Method == http.MethodOptions && reqMethod != "",
		origin:         origin,
		requestMethod:  strings.ToUpper(reqMethod),
		requestHeaders: reqHeaders,
	}
}
