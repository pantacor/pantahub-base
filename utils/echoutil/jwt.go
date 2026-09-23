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
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
)

// Context keys, named as go-json-rest's r.Env entries.
const (
	KeyJWTPayload = "JWT_PAYLOAD"
	KeyRemoteUser = "REMOTE_USER"
)

// JWT authenticates bearer tokens via jwtauth, keeping the WWW-Authenticate
// challenge pvr depends on (echo-jwt sends none).
func JWT(cfg *jwtauth.Config) echo.MiddlewareFunc {
	cfg.ApplyDefaults()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			token, err := cfg.ParseAuthorizationHeader(c.Request().Header.Get("Authorization"))
			if err != nil {
				return unauthorized(c, cfg)
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusInternalServerError, "unexpected JWT claims type")
			}

			// A token bound to an OAuth protected resource (an MCP endpoint) is
			// only good there. It is signed with the same key as every other
			// token, so without this it would pass here too, and what a user
			// granted to one integration would work across the whole API.
			if utils.IsResourceBoundAudience(claims["aud"]) {
				return unauthorized(c, cfg)
			}

			c.Set(KeyJWTPayload, token.Claims)
			c.Set(KeyRemoteUser, claims["id"])

			return next(c)
		}
	}
}

func unauthorized(c *echo.Context, cfg *jwtauth.Config) error {
	c.Response().Header().Set("WWW-Authenticate", cfg.WWWAuthenticate())
	err := Error(c, "Not Authorized", http.StatusUnauthorized)
	return err
}

// Claims returns the JWT claims JWT stored on the context, or nil.
func Claims(c *echo.Context) jwt.MapClaims {
	claims, _ := c.Get(KeyJWTPayload).(jwt.MapClaims)
	return claims
}

// Lookup mirrors a comma-ok r.Env lookup. Keys set by these middlewares are
// never stored as nil, so present means non-nil.
func Lookup(c *echo.Context, key string) (interface{}, bool) {
	v := c.Get(key)
	return v, v != nil
}
