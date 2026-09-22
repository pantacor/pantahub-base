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
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/auth/cimd"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// A connection is what a user created by consenting to an OAuth client that
// named a resource: an MCP client such as claude.ai that may act for them until
// they say otherwise. These endpoints are how they see those and end them.

// oauthConnection is one connected application, as its owner sees it.
type oauthConnection struct {
	ID         string `json:"id"`
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`

	// ClientHost is where a client identified by a URL publishes its identity.
	// Unlike the name it cannot be made up, so it is what to show next to it.
	ClientHost string `json:"client_host,omitempty"`

	Resource string   `json:"resource"`
	Scopes   []string `json:"scopes"`

	GrantedAt  time.Time `json:"granted_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func connectionFrom(token *storage.RefreshToken) oauthConnection {
	connection := oauthConnection{
		ID:         token.FamilyID,
		ClientID:   token.ClientID,
		ClientName: token.ClientName,
		Resource:   token.Resource,
		Scopes:     strings.Fields(token.Scope),
		GrantedAt:  token.GrantedAt,
		LastUsedAt: token.CreatedAt,
		ExpiresAt:  token.ExpiresAt,
	}
	if connection.GrantedAt.IsZero() {
		connection.GrantedAt = token.CreatedAt
	}
	if cimd.IsClientIDURL(token.ClientID) {
		if parsed, err := url.Parse(token.ClientID); err == nil {
			connection.ClientHost = parsed.Hostname()
		}
	}
	if connection.ClientName == "" {
		connection.ClientName = connection.ClientHost
	}
	if connection.ClientName == "" {
		connection.ClientName = token.ClientID
	}
	return connection
}

// connectionsCaller is the account whose connections a request is about. Only
// a person manages connections: a device or a service has none, and a
// resource-bound token never reaches this API at all, so a connected client
// cannot list or end connections, its own included.
func connectionsCaller(c *echo.Context) (string, bool) {
	claims := echoutil.Claims(c)
	accountType, _ := claims["type"].(string)
	if accountType != "USER" && accountType != "SESSION" {
		return "", false
	}
	prn, _ := claims["prn"].(string)
	return prn, prn != ""
}

// HandleListOAuthConnections lists the applications connected to the caller's
// account.
// @Summary List the applications connected to your account
// @Description Lists the OAuth clients (for example MCP clients such as claude.ai) that hold a refresh token for the account, with what they were granted and when they last used it.
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Success 200 {array} oauthConnection
// @Failure 403 {object} utils.RError
// @Router /auth/oauth/connections [get]
func (app *App) HandleListOAuthConnections(c *echo.Context) error {
	caller, ok := connectionsCaller(c)
	if !ok {
		return echoutil.RestErrorWrapper(c, "only a user can list connected applications", http.StatusForbidden)
	}

	repo, err := storage.GetRefreshTokenRepo()
	if err != nil {
		return echoutil.RestErrorWrapper(c, "connected applications are not available", http.StatusInternalServerError)
	}
	tokens, err := repo.ListActive(c.Request().Context(), caller)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "error reading connected applications", http.StatusInternalServerError)
	}

	connections := make([]oauthConnection, 0, len(tokens))
	for i := range tokens {
		connections = append(connections, connectionFrom(&tokens[i]))
	}
	noStore(c)
	return echoutil.WriteJSON(c, http.StatusOK, connections)
}

// HandleDeleteOAuthConnection ends one connection of the caller's account. The
// application's refresh token stops working, and the resource stops accepting
// the access tokens issued under the connection, so the application is cut off
// right away rather than when its current token expires.
// @Summary Disconnect an application from your account
// @Tags auth
// @Security ApiKeyAuth
// @Param id path string true "ID of the connection"
// @Success 204
// @Failure 403 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Router /auth/oauth/connections/{id} [delete]
func (app *App) HandleDeleteOAuthConnection(c *echo.Context) error {
	caller, ok := connectionsCaller(c)
	if !ok {
		return echoutil.RestErrorWrapper(c, "only a user can disconnect applications", http.StatusForbidden)
	}

	repo, err := storage.GetRefreshTokenRepo()
	if err != nil {
		return echoutil.RestErrorWrapper(c, "connected applications are not available", http.StatusInternalServerError)
	}
	// Somebody else's connection and one that does not exist answer alike.
	ended, err := repo.RevokeUserFamily(c.Request().Context(), caller, c.Param("id"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "error disconnecting the application", http.StatusInternalServerError)
	}
	if !ended {
		return echoutil.RestErrorWrapper(c, "no such connected application", http.StatusNotFound)
	}
	return c.NoContent(http.StatusNoContent)
}

// endOAuthConnections ends every connection of an account after a change to
// how it signs in, since whoever held it before may have connected an
// application that would outlive the change. The change itself has succeeded,
// so a failure here is logged rather than reported.
func endOAuthConnections(ctx context.Context, prn, reason string) {
	repo, err := storage.GetRefreshTokenRepo()
	if err == nil {
		err = repo.RevokeUser(ctx, prn)
	}
	if err != nil {
		log.Println("WARNING: " + reason + " could not end the oauth connections of " + prn + ": " + err.Error())
	}
}
