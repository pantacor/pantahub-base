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

// Package jwtauth verifies, mints and refreshes API bearer tokens independently
// of any HTTP framework; utils/echoutil and the auth service use it.
package jwtauth

import (
	"errors"
	"log"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

func init() {
	// Keep a single "aud" as a JSON string, as dgrijalva/jwt-go v3 did; v5
	// defaults to an array. Clients and the object file servers (storage id in
	// aud) read it as a scalar.
	jwt.MarshalSingleStringAsArray = false
}

// Config holds the token settings.
type Config struct {
	// Realm is sent in the 401 challenge; pvr keys stored credentials by it.
	Realm string
	// SigningAlgorithm defaults to HS256.
	SigningAlgorithm string
	// Key signs tokens (and verifies HS* tokens); Pub verifies other algorithms.
	Key interface{}
	Pub interface{}
	// Timeout is the token lifetime (default one hour); MaxRefresh bounds refresh.
	Timeout    time.Duration
	MaxRefresh time.Duration

	// Authenticator checks a username/password login.
	Authenticator func(userID string, password string) bool
	// PayloadFunc returns the claims minted for a user.
	PayloadFunc func(userID string) map[string]interface{}
}

var (
	// ErrNotRefreshable: the token carries no numeric orig_iat (e.g. x509,
	// third-party or implicit tokens).
	ErrNotRefreshable = errors.New("token is not refreshable")
	// ErrTooOld: orig_iat is older than MaxRefresh.
	ErrTooOld = errors.New("token too old to refresh")
)

// Refresh verifies the bearer header and returns the same claims re-signed
// with a new exp of now+timeout. orig_iat is kept, so MaxRefresh bounds the
// chain.
func (c *Config) Refresh(authHeader string, timeout time.Duration) (string, error) {
	token, err := c.ParseAuthorizationHeader(authHeader)
	if err != nil {
		return "", err
	}

	originalClaims := token.Claims.(jwt.MapClaims)
	iat, ok := originalClaims["orig_iat"].(float64)
	if !ok {
		return "", ErrNotRefreshable
	}
	origIat := int64(iat)
	if origIat < time.Now().Add(-c.MaxRefresh).Unix() {
		return "", ErrTooOld
	}

	newToken := jwt.New(jwt.GetSigningMethod(c.SigningAlgorithm))
	newClaims := newToken.Claims.(jwt.MapClaims)
	for key := range originalClaims {
		newClaims[key] = originalClaims[key]
	}
	newClaims["id"] = originalClaims["id"]
	newClaims["exp"] = time.Now().Add(timeout).Unix()
	newClaims["orig_iat"] = origIat
	return newToken.SignedString(c.Key)
}

// ApplyDefaults validates the config and fills defaults.
func (c *Config) ApplyDefaults() {
	if c.Realm == "" {
		log.Fatal("Realm is required")
	}
	if c.SigningAlgorithm == "" {
		c.SigningAlgorithm = "HS256"
	}
	if c.Key == nil && strings.HasPrefix(c.SigningAlgorithm, "HS") {
		log.Fatal("Key required")
	}
	if c.Pub == nil && !strings.HasPrefix(c.SigningAlgorithm, "HS") {
		log.Fatal("Pub Key required")
	}
	if c.Timeout == 0 {
		c.Timeout = time.Hour
	}
}

// ParseAuthorizationHeader verifies a "Bearer <token>" header value. The
// scheme is case-sensitive.
func (c *Config) ParseAuthorizationHeader(authHeader string) (*jwt.Token, error) {
	if authHeader == "" {
		return nil, errors.New("Auth header empty")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if !(len(parts) == 2 && parts[0] == "Bearer") {
		return nil, errors.New("Invalid auth header")
	}

	return jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		if jwt.GetSigningMethod(c.SigningAlgorithm) != token.Method {
			return nil, errors.New("Invalid signing algorithm")
		}
		if strings.HasPrefix(token.Method.Alg(), "HS") {
			return c.Key, nil
		}
		return c.Pub, nil
	})
}

// WWWAuthenticate is the 401 challenge value.
func (c *Config) WWWAuthenticate() string {
	return "JWT realm=" + c.Realm
}
