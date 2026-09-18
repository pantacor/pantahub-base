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

	"github.com/labstack/echo/v5"
)

// If applies ifTrue when condition holds. condition sees the request with the
// mount prefix stripped (e.g. URL.Path "/login" under "/auth").
func If(stripPrefix string, condition func(*http.Request) bool, ifTrue echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		withMW := ifTrue(next)
		return func(c *echo.Context) error {
			if condition(serviceRequest(c.Request(), stripPrefix)) {
				return withMW(c)
			}
			return next(c)
		}
	}
}
