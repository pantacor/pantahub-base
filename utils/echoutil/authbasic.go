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
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// BasicAuthConfig configures AuthBasic.
type BasicAuthConfig struct {
	Realm         string
	Authenticator func(user, password string) bool
}

// AuthBasic answers 401 plus "Basic realm=<Realm>" (realm unquoted) on missing or
// rejected credentials, and 400 without a challenge on a malformed header.
func AuthBasic(mw BasicAuthConfig) echo.MiddlewareFunc {
	if mw.Realm == "" {
		log.Fatal("Realm is required")
	}
	if mw.Authenticator == nil {
		log.Fatal("Authenticator is required")
	}

	unauthorized := func(c *echo.Context) error {
		c.Response().Header().Set("WWW-Authenticate", "Basic realm="+mw.Realm)
		return Error(c, "Not Authorized", http.StatusUnauthorized)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return unauthorized(c)
			}

			user, password, err := decodeBasicAuthHeader(authHeader)
			if err != nil {
				return Error(c, "Invalid authentication", http.StatusBadRequest)
			}

			if !mw.Authenticator(user, password) {
				return unauthorized(c)
			}

			c.Set(KeyRemoteUser, user)
			return next(c)
		}
	}
}

func decodeBasicAuthHeader(header string) (user string, password string, err error) {
	parts := strings.SplitN(header, " ", 2)
	if !(len(parts) == 2 && parts[0] == "Basic") {
		return "", "", errors.New("Invalid authentication")
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.New("Invalid base64")
	}

	creds := strings.SplitN(string(decoded), ":", 2)
	if len(creds) != 2 {
		return "", "", errors.New("Invalid authentication")
	}

	return creds[0], creds[1], nil
}
