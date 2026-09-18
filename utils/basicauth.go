//
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
//

package utils

import (
	"context"
	"net/http"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
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
	jwtConfig *jwtauth.Config,
	mongoClient *mongo.Client,
	ttl time.Duration,
) (string, *RError)

// BasicAuthToBearerMiddleware translates Authorization: Basic headers into
// Authorization: Bearer JWTs using personal access tokens only.
type BasicAuthToBearerMiddleware struct {
	JWT   *jwtauth.Config
	Mongo *mongo.Client
}

// BasicAuthChallenge is sent when Basic credentials are rejected.
const BasicAuthChallenge = `Basic realm="pantahub", Bearer realm="pantahub"`

// BasicAuthResult is the outcome of translating a request's Basic credentials.
type BasicAuthResult int

const (
	// Not Basic, malformed, or no factory: continue, JWT answers the 401.
	BasicAuthPassThrough BasicAuthResult = iota
	// BasicAuthRewritten: valid personal-token credentials; the Authorization
	// header now carries a freshly minted bearer.
	BasicAuthRewritten
	// BasicAuthRejected: Basic credentials that failed validation. The caller
	// must answer 401 with BasicAuthChallenge.
	BasicAuthRejected
)

// Translate swaps valid personal-token Basic credentials for a short-lived bearer
// and returns the user; shared with utils/echoutil.
func (m *BasicAuthToBearerMiddleware) Translate(r *http.Request) (BasicAuthResult, string) {
	authz := r.Header.Get("Authorization")

	// Pass-through: no header, already Bearer, or any other scheme.
	if !strings.HasPrefix(authz, "Basic ") {
		return BasicAuthPassThrough, ""
	}

	user, pass, ok := r.BasicAuth()
	if !ok || user == "" {
		// Malformed header: pass through so downstream JWT middleware
		// produces the canonical 401.
		return BasicAuthPassThrough, ""
	}

	factory := BasicAuthTokenFactory
	if factory == nil {
		return BasicAuthPassThrough, ""
	}

	// Validate username:PERSONAL_TOKEN and mint a short-lived bearer.
	// Personal tokens only — account passwords are rejected by design.
	bearer, rerr := factory(r.Context(), user, pass, m.JWT, m.Mongo, BasicAuthBearerTTL)
	if rerr != nil || bearer == "" {
		return BasicAuthRejected, ""
	}

	// Rewrite header for downstream middleware.
	r.Header.Set("Authorization", "Bearer "+bearer)
	return BasicAuthRewritten, user
}
