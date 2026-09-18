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
	"gitlab.com/pantacor/pantahub-base/utils"
)

// KeyBasicAuthUser mirrors r.Env["PH_BASIC_AUTH_USER"].
const KeyBasicAuthUser = "PH_BASIC_AUTH_USER"

// BasicAuthToBearer mirrors utils.BasicAuthToBearerMiddleware. Must run before JWT.
func BasicAuthToBearer(m *utils.BasicAuthToBearerMiddleware) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			switch result, user := m.Translate(c.Request()); result {
			case utils.BasicAuthRejected:
				c.Response().Header().Set("WWW-Authenticate", utils.BasicAuthChallenge)
				return RestErrorWrapper(c, "Invalid Basic credentials", http.StatusUnauthorized)
			case utils.BasicAuthRewritten:
				c.Set(KeyBasicAuthUser, user)
			}
			return next(c)
		}
	}
}
