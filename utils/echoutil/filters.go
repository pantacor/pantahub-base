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
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ErrJsonPayloadEmpty is returned for a body-less request.
var ErrJsonPayloadEmpty = errors.New("JSON payload is empty")

// DecodeJsonPayload reads the whole body and unmarshals it; an empty body
// returns ErrJsonPayloadEmpty.
func DecodeJsonPayload(c *echo.Context, v interface{}) error {
	body := c.Request().Body
	content, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		return err
	}
	if len(content) == 0 {
		return ErrJsonPayloadEmpty
	}
	return json.Unmarshal(content, v)
}

// UserTypeFilter mirrors utils.UserTypeFilter.
func UserTypeFilter(filterTypes []accounts.AccountType, handler echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		authInfo := AuthInfo(c)
		if authInfo == nil {
			return RestErrorWrapper(c, "Authentication Required", http.StatusUnauthorized)
		}
		if len(filterTypes) > 0 && !utils.AllowsCallerType(filterTypes, authInfo.CallerType) {
			return RestErrorWrapper(c, "Type of user can't realize that action", http.StatusForbidden)
		}
		return handler(c)
	}
}

// UserTypeFilterMW is UserTypeFilter as route middleware (utils.InitUserTypeFilterMiddleware).
func UserTypeFilterMW(filterTypes []accounts.AccountType) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc { return UserTypeFilter(filterTypes, next) }
}

// ScopeFilterMW is ScopeFilter as route middleware (utils.InitScopeFilterMiddleware).
func ScopeFilterMW(filterScopes []utils.Scope) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc { return ScopeFilter(filterScopes, next) }
}
