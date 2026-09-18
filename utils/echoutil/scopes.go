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

// ScopeFilter mirrors utils.ScopeFilter: 401 without a resolved caller, 403
// with the insufficient-scope challenge when no required scope matches.
func ScopeFilter(filterScopes []utils.Scope, handler echo.HandlerFunc) echo.HandlerFunc {
	parsed := utils.MarshalScopes(filterScopes)
	return func(c *echo.Context) error {
		authInfo := AuthInfo(c)
		if authInfo == nil {
			return RestErrorWrapper(c, "Authentication Required", http.StatusUnauthorized)
		}
		if len(parsed) > 0 && !utils.MatchScope(parsed, authInfo.Scopes) {
			c.Response().Header().Set("WWW-Authenticate", utils.ScopeChallenge(parsed))
			return RestErrorWrapper(c, "InSufficient Scopes", http.StatusForbidden)
		}
		return handler(c)
	}
}

// ScopeFilterOptionalAuth mirrors utils.ScopeFilterOptionalAuth: scopes are
// enforced only for authenticated callers.
func ScopeFilterOptionalAuth(filterScopes []utils.Scope, handler echo.HandlerFunc) echo.HandlerFunc {
	parsed := utils.MarshalScopes(filterScopes)
	return func(c *echo.Context) error {
		authInfo := AuthInfo(c)
		if authInfo != nil && len(parsed) > 0 && !utils.MatchScope(parsed, authInfo.Scopes) {
			c.Response().Header().Set("WWW-Authenticate", utils.ScopeChallenge(parsed))
			return RestErrorWrapper(c, "InSufficient Scopes", http.StatusForbidden)
		}
		return handler(c)
	}
}
