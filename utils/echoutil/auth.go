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

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Context keys set by Auth, named as the go-json-rest AuthMiddleware named its
// r.Env entries.
const (
	KeyJWTOrigPayload = "JWT_ORIG_PAYLOAD"
	KeyAuthInfo       = "PH_AUTH_INFO"
)

// Auth mirrors utils.AuthMiddleware (caller resolution incl. call-as) via the
// shared utils.ResolveCaller. Must run after JWT.
func Auth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// A group may let unauthenticated requests through (exports serves
			// public devices); without claims there is no caller to resolve.
			origCallerClaims, ok := c.Get(KeyJWTPayload).(jwt.MapClaims)
			if !ok {
				return next(c)
			}

			callerClaims, authInfo, forbidden := utils.ResolveCaller(origCallerClaims)

			// Set before the rejection check, matching go-json-rest.
			c.Set(KeyJWTPayload, callerClaims)
			c.Set(KeyJWTOrigPayload, origCallerClaims)

			if forbidden != "" {
				return RestErrorWrapper(c, forbidden, http.StatusForbidden)
			}

			c.Set(KeyAuthInfo, authInfo)
			return next(c)
		}
	}
}

// AuthInfo mirrors utils.GetAuthInfo: the resolved caller, or nil when Auth has
// not run.
func AuthInfo(c *echo.Context) *utils.AuthInfo {
	info, ok := c.Get(KeyAuthInfo).(utils.AuthInfo)
	if !ok {
		return nil
	}
	return &info
}
