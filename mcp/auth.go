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

package mcp

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// Scope sets mirror the filters the REST endpoints enforce (devices.Service,
// trails.Service), so a scope-narrowed token holds the same privileges over MCP
// as over REST. Matching is any-of, like utils.MatchScope everywhere else.
var (
	readDeviceScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	})
	readTrailScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.Trails,
		utils.Scopes.ReadTrails,
	})

	// The write sets end in the narrowest scope that unlocks them, which is the
	// one a step-up challenge asks for (see stepUpScopes).
	writeDeviceScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.WriteDevices,
	})
	updateDeviceScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Devices,
		utils.Scopes.UpdateDevices,
	})

	// The REST /apps endpoints take any login of the owner and have no scope of
	// their own. Here a token is a grant to a third party, so looking after a
	// user's OAuth applications is something that user has to grant by name.
	readAppScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.ReadApps,
	})
	writeAppScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.WriteApps,
	})
)

// defaultScopes is what a client is asked to request when it first connects:
// enough to look at devices and nothing that changes anything. Everything else
// is asked for when a tool that needs it is first used (step-up), so a user
// who only ever asks questions never grants more than read access.
func defaultScopes() []string {
	return utils.MarshalScopes([]utils.Scope{
		utils.Scopes.ReadDevices,
		utils.Scopes.ReadTrails,
	})
}

// supportedScopes is everything a token for this endpoint may carry: the
// narrowest scope for each kind of tool, never the catch-all ones. It is what
// the protected resource metadata advertises and what the authorization server
// holds a token for this endpoint to.
func supportedScopes() []string {
	return utils.MarshalScopes([]utils.Scope{
		utils.Scopes.ReadDevices,
		utils.Scopes.ReadTrails,
		utils.Scopes.WriteDevices,
		utils.Scopes.UpdateDevices,
		utils.Scopes.ReadApps,
		utils.Scopes.WriteApps,
		utils.Scopes.WriteTrails,
	})
}

// jwtPublicKey loads the RSA public key the REST API verifies its tokens with,
// once per process, exactly like the MQTT plane does.
var jwtPublicKey = sync.OnceValues(func() (*rsa.PublicKey, error) {
	keys, err := utils.GetJwtRsaKeys("", "")
	if err != nil {
		return nil, err
	}
	return keys.PublicKey, nil
})

// identity is the authenticated caller of a tool.
type identity struct {
	Prn    string
	Nick   string
	Scopes []string
	// ClientID is the OAuth client the token was issued to, when it names
	// one. It is recorded on revisions posted through the endpoint.
	ClientID string
}

const (
	extraNick     = "nick"
	extraClientID = "client_id"
)

// verifyToken is the auth.TokenVerifier of the endpoint. It accepts an RS256
// token signed with the API's key that belongs to a user and was issued for
// this resource.
//
// The audience rule is what keeps tokens from being passed around between
// services: a token minted for another audience (the on-behalf tokens of
// third party services carry their service PRN) is refused here, and a token
// without any audience — an ordinary API login — only passes when
// PANTAHUB_MCP_ALLOW_API_TOKENS says so.
func (s *Service) verifyToken(ctx context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
	pub, err := jwtPublicKey()
	if err != nil {
		return nil, err
	}

	token, err := jwtgo.Parse(raw, func(t *jwtgo.Token) (interface{}, error) {
		if jwtgo.GetSigningMethod("RS256") != t.Method {
			return nil, errors.New("invalid signing algorithm")
		}
		return pub, nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, fmt.Errorf("%w: signature or validity check failed", auth.ErrInvalidToken)
	}
	claims, ok := token.Claims.(jwtgo.MapClaims)
	if !ok {
		return nil, fmt.Errorf("%w: unreadable claims", auth.ErrInvalidToken)
	}

	audiences := utils.TokenAudiences(claims["aud"])
	switch {
	case len(audiences) == 0:
		if !s.allowAPITokens {
			return nil, fmt.Errorf("%w: token was not issued for this resource", auth.ErrInvalidToken)
		}
	case !s.isAudience(audiences):
		return nil, fmt.Errorf("%w: token was issued for another audience", auth.ErrInvalidToken)
	default:
		// A bound token names the authorization server that issued it.
		if issuer, _ := claims["iss"].(string); issuer != utils.GetAPIEndpoint("") {
			return nil, fmt.Errorf("%w: token was issued by another authorization server", auth.ErrInvalidToken)
		}
		// And the connection it was issued under, which the user may have
		// ended since. The token is self-contained and would otherwise keep
		// working until it expires. A 401 makes the client try its refresh
		// token, which is revoked too, and then ask the user to connect again.
		connectionID, _ := claims[claimConnectionID].(string)
		if connectionID == "" {
			return nil, fmt.Errorf("%w: token names no connection", auth.ErrInvalidToken)
		}
		active, err := s.connections.active(ctx, connectionID)
		if err != nil {
			return nil, err
		}
		if !active {
			return nil, fmt.Errorf("%w: the connection was ended", auth.ErrInvalidToken)
		}
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, fmt.Errorf("%w: token has no expiry", auth.ErrInvalidToken)
	}

	// Identity comes from the effective claims, so an admin token that acts as
	// another account on the REST API acts as that same account here.
	effective := effectiveClaims(claims)

	accountType, _ := effective["type"].(string)
	if accountType != "USER" && accountType != "SESSION" {
		return nil, fmt.Errorf("%w: a user token is required", auth.ErrInvalidToken)
	}
	prn, _ := effective["prn"].(string)
	if prn == "" {
		return nil, fmt.Errorf("%w: token names no account", auth.ErrInvalidToken)
	}
	scopes, _ := effective["scopes"].(string)
	nick, _ := effective["nick"].(string)
	// The client a bound token was issued to is a claim of the token itself,
	// not of the account it acts as.
	clientID, _ := claims["client_id"].(string)

	return &auth.TokenInfo{
		Scopes:     strings.Fields(scopes),
		Expiration: time.Unix(int64(exp), 0),
		UserID:     prn,
		Extra:      map[string]any{extraNick: nick, extraClientID: clientID},
	}, nil
}

// effectiveClaims resolves admin impersonation the same way
// utils.AuthMiddleware does.
func effectiveClaims(claims jwtgo.MapClaims) jwtgo.MapClaims {
	callAs, ok := claims["call-as"].(map[string]interface{})
	if !ok {
		return claims
	}
	return jwtgo.MapClaims(callAs)
}

func (s *Service) isAudience(audiences []string) bool {
	for _, audience := range audiences {
		canonical, err := canonicalResourceURL(audience)
		if err == nil && canonical == s.resourceURL {
			return true
		}
	}
	return false
}

// identityFrom turns the token the bearer middleware verified into the caller
// of a tool. It fails closed: no token info, no identity.
func identityFrom(info *auth.TokenInfo) (*identity, error) {
	if info == nil || info.UserID == "" {
		return nil, errors.New("not authenticated")
	}
	nick, _ := info.Extra[extraNick].(string)
	clientID, _ := info.Extra[extraClientID].(string)
	return &identity{Prn: info.UserID, Nick: nick, Scopes: info.Scopes, ClientID: clientID}, nil
}

// requireToolScopes answers a tool call the token's scopes do not cover with
// the HTTP 403 insufficient_scope challenge of RFC 6750, naming the scopes that
// would do. MCP clients act on that challenge to ask the user for more access
// (step-up authorization); a JSON-RPC error inside a 200 would not trigger it.
//
// It has to peek at the JSON-RPC body to learn which tool is called. Anything
// it cannot read as a single tools/call request is passed on untouched, which
// is safe because every tool handler checks its scopes again (see authorize).
func (s *Service) requireToolScopes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		var call struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(body, &call) == nil && call.Method == "tools/call" {
			if required, known := toolScopes[call.Params.Name]; known {
				info := auth.TokenInfoFromContext(r.Context())
				if info == nil || !utils.MatchScope(required, info.Scopes) {
					w.Header().Set("WWW-Authenticate", fmt.Sprintf(
						`Bearer error="insufficient_scope", scope=%q, resource_metadata=%q`,
						strings.Join(stepUpScopes(required), " "), s.metadataURL))
					http.Error(w, "insufficient scope", http.StatusForbidden)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// stepUpScopes is what a 403 challenge asks for: everything the endpoint
// advertises plus the narrowest scope that unlocks the refused tool. Clients
// do not reliably carry earlier step-up scopes forward, so the challenge names
// the full set still needed rather than only the missing one.
func stepUpScopes(required []string) []string {
	scopes := defaultScopes()
	narrowest := required[len(required)-1]
	for _, scope := range scopes {
		if scope == narrowest {
			return scopes
		}
	}
	return append(scopes, narrowest)
}
