// Copyright (c) 2026 Pantacor Ltd.
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

package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gosimple/slug"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth/cimd"
	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnvOAuthDCRAllowedRedirectHosts lists, comma separated, the hosts a
// dynamically registered client may redirect to, such as
// "claude.ai,127.0.0.1,localhost". Registration is anonymous, so empty turns
// it off.
const EnvOAuthDCRAllowedRedirectHosts = "PANTAHUB_OAUTH_DCR_ALLOWED_REDIRECT_HOSTS"

const (
	dcrMaxRedirectURIs = 10
	dcrMaxURILength    = 2048

	// A registration nobody connected with, or whose connections have all
	// expired, is removed after this long. Clients register again when told
	// their client_id is unknown.
	dcrUnusedClientTTL = 7 * 24 * time.Hour
	dcrSweepInterval   = time.Hour
)

var (
	// Registration is unauthenticated, so it is throttled per address and in
	// total: each call writes a record.
	dcrPerClientLimiter = utils.NewIPRateLimiter(10.0/3600, 5)
	dcrGlobalLimiter    = utils.NewIPRateLimiter(500.0/3600, 100)

	dcrLastSweep atomic.Int64
)

// dcrEnabled reports whether clients may register themselves. That is only
// useful, and only offered, when there is a resource to bind their tokens to
// (the MCP endpoint, when it is enabled) and hosts they may redirect to.
func (app *App) dcrEnabled() bool {
	return len(app.oauthResources) > 0 && len(dcrAllowedRedirectHosts()) > 0
}

func dcrAllowedRedirectHosts() map[string]bool {
	hosts := map[string]bool{}
	for _, entry := range strings.Split(utils.GetEnvDefault(EnvOAuthDCRAllowedRedirectHosts, ""), ",") {
		if entry = strings.ToLower(strings.TrimSpace(entry)); entry != "" {
			hosts[entry] = true
		}
	}
	return hosts
}

// ClientRegistrationRequest is the RFC 7591 request payload.
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// ClientRegistrationResponse is the RFC 7591 response payload.
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

func dcrError(c *echo.Context, status int, code, description string) error {
	noStore(c)
	return echoutil.WriteJSON(c, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

// HandleOAuthRegister implements RFC 7591 OAuth 2.0 Dynamic Client Registration.
// Registered clients are marked dynamic: they may only redirect to the hosts in
// EnvOAuthDCRAllowedRedirectHosts, matched exactly, and only ever get
// resource-bound tokens.
func (app *App) HandleOAuthRegister(c *echo.Context) error {
	if !app.dcrEnabled() {
		return dcrError(c, http.StatusNotFound, "not_found", "Dynamic client registration is disabled")
	}

	req := ClientRegistrationRequest{}
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		return dcrError(c, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON payload")
	}

	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > dcrMaxRedirectURIs {
		return dcrError(c, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("redirect_uris must list between 1 and %d URIs", dcrMaxRedirectURIs))
	}
	for _, rawURI := range req.RedirectURIs {
		if err := validateDCRRedirectURI(rawURI); err != nil {
			return dcrError(c, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		}
	}

	// Token endpoint auth method: public clients use "none"
	authMethod := strings.TrimSpace(req.TokenEndpointAuthMethod)
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" {
		return dcrError(c, http.StatusBadRequest, "invalid_client_metadata", "unsupported token_endpoint_auth_method: only 'none' (public PKCE) is supported")
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{grantAuthorizationCode, grantRefreshToken}
	}
	for _, gt := range grantTypes {
		if gt != grantAuthorizationCode && gt != grantRefreshToken {
			return dcrError(c, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type")
		}
	}

	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	for _, rt := range responseTypes {
		if rt != "code" {
			return dcrError(c, http.StatusBadRequest, "invalid_client_metadata", "unsupported response_type: only 'code' is supported")
		}
	}

	// Self-asserted labels, shown on the consent page.
	clientName := cimd.Printable(req.ClientName, 80)
	if clientName == "" {
		clientName = "MCP Client"
	}
	clientURI := cimd.SafeLink(req.ClientURI)
	logoURI := cimd.SafeLink(req.LogoURI)

	now := time.Now()
	objID := primitive.NewObjectID()
	appPrn := apps.Prn + objID.Hex()

	nick := slug.Make(clientName)
	if nick == "" {
		nick = "client"
	}
	nick = fmt.Sprintf("%s-%s", nick, objID.Hex())

	tpApp := &apps.TPApp{
		ID:           objID,
		Name:         clientName,
		Nick:         nick,
		Prn:          appPrn,
		Type:         apps.AppTypePKCE,
		Owner:        "", // unauthenticated dynamic registration
		Logo:         logoURI,
		RedirectURIs: req.RedirectURIs,
		// No scopes of its own: what it may carry is what the resource
		// it names allows.
		Dynamic:      true,
		TimeCreated:  now,
		TimeModified: now,
	}

	// Only what gets written is throttled: each registration is a record.
	if !dcrPerClientLimiter.Allow(utils.ClientIP(c.Request())) || !dcrGlobalLimiter.Allow("") {
		c.Response().Header().Set("Retry-After", "3600")
		return dcrError(c, http.StatusTooManyRequests, "too_many_requests", "Too many client registrations, try again later")
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	if _, err := apps.CreateOrUpdateApp(ctx, tpApp, app.mongoClient.Database(utils.MongoDb)); err != nil {
		log.Println("ERROR: dynamic client registration failed: " + err.Error())
		return dcrError(c, http.StatusInternalServerError, "server_error", "Failed to register client")
	}
	app.sweepUnusedDynamicClients()

	noStore(c)
	c.Response().Header().Set("Pragma", "no-cache")
	return echoutil.WriteJSON(c, http.StatusCreated, ClientRegistrationResponse{
		ClientID:                appPrn,
		ClientIDIssuedAt:        now.Unix(),
		ClientName:              clientName,
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		ClientURI:               clientURI,
		LogoURI:                 logoURI,
	})
}

// validateDCRRedirectURI admits a redirect URI of a self-registered client
// only on an allow listed host, and plain http only towards loopback.
func validateDCRRedirectURI(rawURI string) error {
	if len(rawURI) > dcrMaxURILength {
		return errors.New("redirect URI is too long")
	}
	if err := redirecturi.ValidateURI(rawURI); err != nil {
		return errors.New("invalid redirect URI: " + err.Error())
	}
	u, err := url.Parse(rawURI)
	if err != nil {
		return errors.New("invalid redirect URI")
	}
	if u.Scheme == "http" && !redirecturi.IsLoopbackHost(u.Hostname()) {
		return errors.New("http redirect URIs are only permitted on loopback")
	}
	if !dcrAllowedRedirectHosts()[strings.ToLower(u.Hostname())] {
		return errors.New("redirect URI host is not allowed on this server")
	}
	return nil
}

// sweepUnusedDynamicClients removes, at most once an hour and in the
// background, dynamic registrations older than dcrUnusedClientTTL that hold no
// unexpired refresh token.
func (app *App) sweepUnusedDynamicClients() {
	now := time.Now()
	last := dcrLastSweep.Load()
	if now.Sub(time.Unix(0, last)) < dcrSweepInterval || !dcrLastSweep.CompareAndSwap(last, now.UnixNano()) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		collection := app.mongoClient.Database(utils.MongoDb).Collection(apps.DBCollection)
		cur, err := collection.Find(ctx,
			bson.M{"dynamic": true, "time-created": bson.M{"$lt": now.Add(-dcrUnusedClientTTL)}},
			options.Find().SetProjection(bson.M{"prn": 1}).SetLimit(1000))
		if err != nil {
			log.Println("WARNING: dynamic client sweep: " + err.Error())
			return
		}
		var stale []struct {
			Prn string `bson:"prn"`
		}
		if err := cur.All(ctx, &stale); err != nil || len(stale) == 0 {
			return
		}
		prns := make([]string, 0, len(stale))
		for _, client := range stale {
			prns = append(prns, client.Prn)
		}

		repo, err := storage.GetRefreshTokenRepo()
		if err != nil {
			return
		}
		inUse, err := repo.ClientsInUse(ctx, prns)
		if err != nil {
			log.Println("WARNING: dynamic client sweep: " + err.Error())
			return
		}
		unused := []string{}
		for _, prn := range prns {
			if !inUse[prn] {
				unused = append(unused, prn)
			}
		}
		if len(unused) == 0 {
			return
		}
		if _, err := collection.DeleteMany(ctx, bson.M{"dynamic": true, "prn": bson.M{"$in": unused}}); err != nil {
			log.Println("WARNING: dynamic client sweep: " + err.Error())
		}
	}()
}

// markLegacyDynamicClients flags the clients the first version of dynamic
// registration created without the dynamic marker, so they lose the
// account-wide scopes it gave them. They are recognised by what only that
// code produced: no owner, a PKCE type and a nick ending in the object id.
func markLegacyDynamicClients(ctx context.Context, database *mongo.Database) (int64, error) {
	result, err := database.Collection(apps.DBCollection).UpdateMany(ctx,
		bson.M{
			"owner":   bson.M{"$in": bson.A{"", nil}},
			"type":    apps.AppTypePKCE,
			"dynamic": bson.M{"$ne": true},
			"nick":    bson.M{"$regex": `-[0-9a-f]{24}$`},
		},
		bson.M{"$set": bson.M{"dynamic": true}, "$unset": bson.M{"scopes": ""}})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}
