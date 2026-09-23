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

package metrics

import (
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// EchoMiddleware mirrors Middleware for echo services, keeping the same labels.
// stripPrefix is the service mount (e.g. "/dash"). Must wrap echoutil.Instrument.
func EchoMiddleware(stripPrefix string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if err := next(c); err != nil {
				return err
			}
			endpoint := c.Request().URL.Path
			if p := strings.TrimPrefix(endpoint, stripPrefix); len(p) < len(endpoint) {
				endpoint = p
			}
			observe(endpoint, c.Request().Method, c.Get(echoutil.KeyStatusCode).(int), c.Get(echoutil.KeyElapsedTime).(*time.Duration))
			return nil
		}
	}
}
