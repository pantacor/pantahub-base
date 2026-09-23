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

package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/oauth"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo"
)

type connectedProviderConnectRequest struct {
	Service    string `json:"service"`
	RedirectTo string `json:"redirect_uri"`
}

type connectedProviderDisconnectRequest struct {
	Service    string `json:"service"`
	ProviderID string `json:"provider_id"`
}

type connectedProviderResponse struct {
	Service     string    `json:"service"`
	Email       string    `json:"email,omitempty"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
}

func socialConnectAccountPRN(c *echo.Context) (string, error) {
	authInfo := echoutil.AuthInfo(c)
	if authInfo == nil || authInfo.Caller == "" {
		return "", errors.New("you need to be logged in")
	}
	return string(authInfo.Caller), nil
}

// handleGetConnectedProviders lists the external identities connected to the
// authenticated account.
func (a *App) handleGetConnectedProviders(c *echo.Context) error {
	accountPRN, err := socialConnectAccountPRN(c)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusUnauthorized)
	}

	providers, err := listConnectedProviders(c.Request().Context(), accountPRN, a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts"))
	if err != nil {
		status := http.StatusInternalServerError
		if err == mongo.ErrNoDocuments {
			status = http.StatusNotFound
		}
		return echoutil.RestError(c, err, "Unable to list connected providers", status)
	}
	response := make([]connectedProviderResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, connectedProviderResponse{
			Service:     provider.Service,
			Email:       provider.Email,
			ConnectedAt: provider.ConnectedAt,
		})
	}
	return echoutil.WriteJSON(c, http.StatusOK, response)
}

// handlePostConnectedProvider starts an authenticated OAuth connect flow. The
// signed cookie binds the callback to the account that initiated this request;
// the callback never trusts an account PRN supplied by the browser.
func (a *App) handlePostConnectedProvider(c *echo.Context) error {
	accountPRN, err := socialConnectAccountPRN(c)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusUnauthorized)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	account, err := getUserByPRN(c.Request().Context(), accountPRN, collection)
	if err != nil {
		return echoutil.RestError(c, err, "Account not found", http.StatusUnauthorized)
	}
	if account.Type != accounts.AccountTypeUser && account.Type != accounts.AccountTypeAdmin {
		return echoutil.RestError(c, nil, "This account type cannot connect OAuth providers", http.StatusForbidden)
	}

	payload := connectedProviderConnectRequest{}
	if strings.Contains(strings.ToLower(c.Request().Header.Get("Content-Type")), "application/json") {
		if err := echoutil.DecodeJsonPayload(c, &payload); err != nil {
			return echoutil.RestError(c, err, "Invalid connect payload", http.StatusBadRequest)
		}
	} else {
		_ = c.Request().ParseForm()
		payload.Service = c.Request().FormValue("service")
		payload.RedirectTo = c.Request().FormValue("redirect_uri")
	}

	service := oauth.ServiceType(strings.ToLower(strings.TrimSpace(payload.Service)))
	if _, ok := oauth.ServicesConfigs[service]; !ok {
		return echoutil.RestError(c, nil, "We can't connect to that service", http.StatusBadRequest)
	}

	redirectTo := payload.RedirectTo
	if queryRedirect := c.Request().URL.Query().Get("redirect_uri"); queryRedirect != "" {
		redirectTo = queryRedirect
	}
	if redirectTo != "" {
		audit := auditContext(c.Request(), "social_connect")
		audit.Service = string(service)
		if err := validateSocialRedirectURI(redirectTo, audit); err != nil {
			return echoutil.RestError(c, err, err.Error(), http.StatusBadRequest)
		}
	}

	// The authenticated account PRN is bound into the signed OAuth state rather
	// than a cross-site cookie: the hub and API are on different registrable
	// domains, so the browser drops such cookies. The callback trusts only a PRN
	// we signed here, and the caller is authenticated at this point.
	authorizeURL, err := oauth.AuthorizationURLByServiceWithConnect(service, redirectTo, accountPRN)
	if err != nil {
		return echoutil.RestError(c, err, "Unable to start OAuth connect flow", http.StatusInternalServerError)
	}
	return echoutil.WriteJSON(c, http.StatusOK, map[string]string{"authorize_url": authorizeURL})
}

// handleDeleteConnectedProvider disconnects one provider identity. A missing
// provider_id is accepted for compatibility and removes all identities for
// the requested service.
func (a *App) handleDeleteConnectedProvider(c *echo.Context) error {
	accountPRN, err := socialConnectAccountPRN(c)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusUnauthorized)
	}

	payload := connectedProviderDisconnectRequest{}
	if strings.Contains(strings.ToLower(c.Request().Header.Get("Content-Type")), "application/json") {
		if err := echoutil.DecodeJsonPayload(c, &payload); err != nil {
			return echoutil.RestError(c, err, "Invalid disconnect payload", http.StatusBadRequest)
		}
	} else {
		_ = c.Request().ParseForm()
		payload.Service = c.Request().FormValue("service")
		payload.ProviderID = c.Request().FormValue("provider_id")
	}
	if payload.Service == "" {
		payload.Service = c.Request().URL.Query().Get("service")
	}
	if payload.ProviderID == "" {
		payload.ProviderID = c.Request().URL.Query().Get("provider_id")
	}
	service := strings.ToLower(strings.TrimSpace(payload.Service))
	if service == "" {
		return echoutil.RestError(c, nil, "Provider service is required", http.StatusBadRequest)
	}

	if err := disconnectProvider(c.Request().Context(), accountPRN, service, payload.ProviderID,
		a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")); err != nil {
		status := http.StatusInternalServerError
		if err == mongo.ErrNoDocuments {
			status = http.StatusNotFound
		}
		return echoutil.RestError(c, err, "Connected provider not found", status)
	}
	return echoutil.WriteJSON(c, http.StatusOK, true)
}

func redirectAfterProviderConnect(c *echo.Context, redirectTo string, provider accounts.ConnectedProvider) error {
	if redirectTo == "" {
		return echoutil.WriteJSON(c, http.StatusOK, provider)
	}
	u, err := url.Parse(redirectTo)
	if err != nil {
		return echoutil.RestError(c, err, "Invalid redirect URI", http.StatusBadRequest)
	}
	query := u.Query()
	query.Set("connected_provider", provider.Service)
	u.RawQuery = query.Encode()
	http.Redirect(c.Response(), c.Request(), u.String(), http.StatusTemporaryRedirect)
	return nil
}
