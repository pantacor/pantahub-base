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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrUnknownClient is returned when client_id does not resolve to a registered
// application or to a service account.
var ErrUnknownClient = errors.New("unknown client_id")

// registeredCallbacks resolves clientID to the callback URLs registered for it.
//
// Applications created through /apps carry their own redirect_uris. Older
// service accounts keep the equivalent list on the account itself, so both are
// consulted before giving up.
func (a *App) registeredCallbacks(ctx context.Context, clientID string) ([]string, error) {
	if clientID == "" {
		return nil, ErrUnknownClient
	}

	app, _, err := apps.SearchApp(ctx, "", clientID, a.mongoClient.Database(utils.MongoDb))
	if err == nil && app != nil {
		return app.RedirectURIs, nil
	}

	account, err := authservices.GetAccount(clientID, a.mongoClient)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUnknownClient
		}
		return nil, err
	}

	return account.Oauth2RedirectURIs, nil
}

// validateRedirectURI rejects any redirect target that is not registered
// against clientID.
//
// A client_id that resolves to nothing is rejected outright. That is a
// different case from an application that exists but declares no callbacks:
// the latter is deliberately left unconstrained for compatibility, and routing
// an unknown client through that path would let any caller opt out of the check
// simply by naming an application that does not exist.
func (a *App) validateRedirectURI(ctx context.Context, clientID, candidate string, audit redirecturi.AuditContext) error {
	audit.ClientID = clientID

	registered, err := a.registeredCallbacks(ctx, clientID)
	if err != nil {
		if err == ErrUnknownClient {
			redirecturi.LogRejection(candidate, ErrUnknownClient, audit)
			return ErrUnknownClient
		}
		return err
	}

	return redirecturi.Validate(registered, candidate, audit)
}

// EnvPantahubSocialRedirectOrigins optionally overrides the origins the social
// login flow may return to, as a comma separated list. When unset the origin is
// derived from the web interface this deployment is paired with.
const EnvPantahubSocialRedirectOrigins = "PANTAHUB_SOCIAL_REDIRECT_ORIGINS"

// socialRedirectOrigins returns the origins the social login flow is allowed to
// return to.
//
// The callback delivers a signed-in user token in the fragment, so the only
// acceptable destination is our own web interface: PANTAHUB_HOST_WWW for this
// deployment, or the explicit override when one deployment fronts more than one
// UI origin. Third party applications must use the code or PKCE flows, which
// validate against their own registered callback URLs and issue scoped grants.
func socialRedirectOrigins() []string {
	if override := utils.GetEnv(EnvPantahubSocialRedirectOrigins); override != "" {
		origins := []string{}
		for _, entry := range strings.Split(override, ",") {
			if trimmed := strings.TrimSpace(entry); trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
		return origins
	}

	wwwHost := utils.GetEnv(utils.EnvPantahubWWWHost)
	if wwwHost == "" {
		return nil
	}

	scheme := utils.GetEnv(utils.EnvPantahubScheme)
	if scheme == "" {
		scheme = "https"
	}

	origin := scheme + "://" + wwwHost
	if port := utils.GetEnv(utils.EnvPantahubPort); port != "" && port != "80" && port != "443" {
		origin += ":" + port
	}

	return []string{origin}
}

// validateSocialRedirectURI rejects any social login return target that is not
// on one of this deployment's web interface origins.
func validateSocialRedirectURI(candidate string, audit redirecturi.AuditContext) error {
	return redirecturi.ValidateOrigin(socialRedirectOrigins(), candidate, audit)
}

// auditContext captures the request metadata attached to rejection events.
func auditContext(r *rest.Request, flow string) redirecturi.AuditContext {
	return redirecturi.AuditContext{
		Flow:       flow,
		RemoteAddr: r.Request.RemoteAddr,
		UserAgent:  r.Request.UserAgent(),
	}
}
