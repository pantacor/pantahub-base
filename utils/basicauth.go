//
// Copyright 2026 Pantacor Ltd.
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
//

package utils

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/mongo"
)

// BasicAuthBearerTTL is the lifetime of JWTs minted from Basic auth headers.
// Short by design: the bearer is consumed on the very next hop in the
// middleware chain.
const BasicAuthBearerTTL = 30 * time.Second

// BasicAuthTokenFactory creates a bearer JWT from Basic auth credentials.
// It is injected during application initialization (e.g. in base/init.go)
// to avoid circular package imports.
var BasicAuthTokenFactory func(
	ctx context.Context,
	username string,
	password string,
	jwtMiddleware *jwt.JWTMiddleware,
	mongoClient *mongo.Client,
	ttl time.Duration,
) (string, *RError)

// BasicAuthToBearerMiddleware translates Authorization: Basic headers into
// Authorization: Bearer JWTs using personal access tokens only.
type BasicAuthToBearerMiddleware struct {
	JWT   *jwt.JWTMiddleware
	Mongo *mongo.Client
}

func (m *BasicAuthToBearerMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {
	return func(w rest.ResponseWriter, r *rest.Request) {
		authz := r.Header.Get("Authorization")

		// Pass-through: no header, already Bearer, or any other scheme.
		if !strings.HasPrefix(authz, "Basic ") {
			h(w, r)
			return
		}

		user, pass, ok := r.Request.BasicAuth()
		if !ok || user == "" {
			// Malformed header: pass through so downstream JWT middleware
			// produces the canonical 401.
			h(w, r)
			return
		}

		factory := BasicAuthTokenFactory
		if factory == nil {
			h(w, r)
			return
		}

		// Validate username:PERSONAL_TOKEN and mint a short-lived bearer.
		// Personal tokens only — account passwords are rejected by design.
		bearer, rerr := factory(r.Context(), user, pass, m.JWT, m.Mongo, BasicAuthBearerTTL)
		if rerr != nil || bearer == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="pantahub", Bearer realm="pantahub"`)
			RestErrorWrapper(w, "Invalid Basic credentials", http.StatusUnauthorized)
			return
		}

		// Rewrite header for downstream middleware.
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Env["PH_BASIC_AUTH_USER"] = user

		h(w, r)
	}
}
