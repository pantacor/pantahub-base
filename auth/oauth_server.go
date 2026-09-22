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
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/cimd"
	"gitlab.com/pantacor/pantahub-base/auth/pkceservice"
	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// This file is what lets a standards-following OAuth client that was never
// registered here (an MCP client such as claude.ai) complete the PKCE flow in
// pkce.go on its own: it finds the endpoints through the metadata document,
// introduces itself with a client metadata document URL, names the resource it
// wants a token for, and keeps its session alive with refresh tokens.
//
// Everything here is opt-in per request. A client that sends no resource and
// has a registered client_id gets exactly the behaviour it got before.
//
// A resource-bound grant always comes with a refresh token, since the clients
// it exists for expect one; offline_access is therefore neither advertised nor
// needed, and the user ends a connection from their connected applications.

const (
	// AuthorizationServerMetadataPath is where RFC 8414 puts the metadata of an
	// authorization server whose issuer has no path.
	AuthorizationServerMetadataPath = "/.well-known/oauth-authorization-server"

	// EnvOAuthRefreshTokenDays is how long a refresh token lasts when unused.
	// Every use replaces it with a new one that lasts this long again.
	//#nosec G101 -- the name of an environment variable, not a token value
	EnvOAuthRefreshTokenDays = "PANTAHUB_OAUTH_REFRESH_TOKEN_DAYS"

	defaultRefreshTokenDays = 30

	grantAuthorizationCode = "authorization_code"
	grantRefreshToken      = "refresh_token"

	// ClaimConnectionID is the claim of a resource-bound access token that
	// names the connection it was issued under.
	ClaimConnectionID = "cnx"
)

// OAuthResource is a protected resource this server issues audience-bound
// tokens for.
type OAuthResource struct {
	// URL identifies the resource. It becomes the aud claim.
	URL string

	// Scopes are the scopes a token for this resource may carry. A token bound
	// to a resource never carries anything else, whatever the client asks for
	// and whatever the account could otherwise do.
	Scopes []string

	// DefaultScopes are granted when the client names none.
	DefaultScopes []string
}

// RegisterOAuthResource makes resource a valid target of the RFC 8707 resource
// parameter. It is meant to be called while the API is being wired up.
func (app *App) RegisterOAuthResource(resource OAuthResource) error {
	canonical, err := utils.CanonicalResourceURL(resource.URL)
	if err != nil {
		return err
	}
	resource.URL = canonical

	if app.oauthResources == nil {
		app.oauthResources = map[string]OAuthResource{}
	}
	app.oauthResources[canonical] = resource
	return nil
}

// errInvalidTarget is RFC 8707's error for a resource this server does not
// issue tokens for.
var errInvalidTarget = errors.New("the requested resource is not served by this authorization server")

// resolveResource looks up the resource a client named. No resource is not an
// error: it is what every client did before resources existed.
func (app *App) resolveResource(raw string) (*OAuthResource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	canonical, err := utils.CanonicalResourceURL(raw)
	if err != nil {
		return nil, errInvalidTarget
	}
	resource, ok := app.oauthResources[canonical]
	if !ok {
		return nil, errInvalidTarget
	}
	return &resource, nil
}

// oauthIssuer identifies this authorization server. Tokens bound to a resource
// carry it as iss, and the metadata document is published under it.
func oauthIssuer() string {
	return utils.GetAPIEndpoint("")
}

// grantedScopes narrows what a client asked for to what a token for resource
// may carry. Scope names are accepted with or without the API prefix, like
// everywhere else. Asking for nothing the resource offers yields its defaults,
// never the account-wide scope an unbound token gets.
func grantedScopes(resource *OAuthResource, requested string) []string {
	allowed := map[string]bool{}
	for _, scope := range resource.Scopes {
		allowed[scope] = true
	}

	granted := []string{}
	seen := map[string]bool{}
	for _, scope := range utils.ScopeStringFilterBy(strings.Fields(requested), "", "") {
		if allowed[scope] && !seen[scope] {
			seen[scope] = true
			granted = append(granted, scope)
		}
	}
	if len(granted) == 0 {
		granted = append(granted, resource.DefaultScopes...)
	}
	return granted
}

func refreshTokenTTL() time.Duration {
	days, err := strconv.Atoi(utils.GetEnvDefault(EnvOAuthRefreshTokenDays, ""))
	if err != nil || days <= 0 {
		days = defaultRefreshTokenDays
	}
	return time.Duration(days) * 24 * time.Hour
}

func accessTokenLifetime() time.Duration {
	minutes, err := strconv.Atoi(utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes))
	if err != nil || minutes <= 0 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

// mintBoundAccessToken signs an access token for account that is only good at
// resource. Unlike a login token it names its issuer, its audience and the
// client it was issued to, carries only scopes the resource allows, and has no
// orig_iat: it is renewed with a refresh token, not by GET /auth/login.
func (app *App) mintBoundAccessToken(account accounts.Account, resource *OAuthResource, scopes []string, clientID, connectionID string) (string, time.Duration, error) {
	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwtConfig.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	for key, value := range authservices.AccountToPayload(account) {
		claims[key] = value
	}

	lifetime := accessTokenLifetime()
	now := time.Now()
	claims["scopes"] = strings.Join(scopes, " ")
	claims["iss"] = oauthIssuer()
	claims["aud"] = resource.URL
	claims["client_id"] = clientID
	// The connection this token was issued under. The resource checks it, so
	// that ending a connection takes effect at once and not when the token
	// expires.
	claims[ClaimConnectionID] = connectionID
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(lifetime).Unix()

	signed, err := token.SignedString(app.jwtConfig.Key)
	if err != nil {
		return "", 0, err
	}
	return signed, lifetime, nil
}

// tokenRequest is the body of POST /auth/oauth/token.
type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	AccessCode   string `json:"access-code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
	Resource     string `json:"resource"`
	Scope        string `json:"scope"`

	// standard is set for a form encoded request, the mark of a client written
	// against OAuth rather than against this API.
	standard bool
}

// oauthError writes the error response RFC 6749 section 5.2 specifies. The
// error helpers used elsewhere in this API answer with an incident id in the
// error field and keep the reason for the logs; an OAuth client acts on the
// code itself, above all on invalid_grant, which is what tells it to stop
// retrying and send the user through consent again.
func oauthError(c *echo.Context, status int, code, description string) error {
	noStore(c)
	return echoutil.WriteJSON(c, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

// fail answers a token request in the shape its sender understands: RFC 6749
// for a standard client, this API's own error shape for the JSON clients that
// were written against it.
func (req *tokenRequest) fail(c *echo.Context, code, description string, status int) error {
	if req.standard {
		return oauthError(c, status, code, description)
	}
	return echoutil.RestErrorWrapperUser(c, code, description, status)
}

// failRedirectMismatch keeps the error code older clients may match on, which
// OAuth does not define; a standard client gets the one it does.
func (req *tokenRequest) failRedirectMismatch(c *echo.Context) error {
	const description = "Provided redirect_uri does not match the one in the authorization request"
	if req.standard {
		return oauthError(c, http.StatusBadRequest, "invalid_grant", description)
	}
	return echoutil.RestErrorWrapperUser(c, "invalid_redirect_uri", description, http.StatusBadRequest)
}

// decodeTokenRequest reads a token request in either encoding. OAuth specifies
// application/x-www-form-urlencoded and standard clients send nothing else;
// the clients written against this endpoint before send JSON, and still may.
func decodeTokenRequest(c *echo.Context, req *tokenRequest) error {
	contentType := strings.ToLower(c.Request().Header.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		return echoutil.DecodeJsonPayload(c, req)
	}

	req.standard = true
	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	form := c.Request().PostForm
	req.GrantType = form.Get("grant_type")
	req.Code = form.Get("code")
	req.AccessCode = form.Get("access-code")
	req.CodeVerifier = form.Get("code_verifier")
	req.RedirectURI = form.Get("redirect_uri")
	req.ClientID = form.Get("client_id")
	req.RefreshToken = form.Get("refresh_token")
	req.Resource = form.Get("resource")
	req.Scope = form.Get("scope")
	return nil
}

// completeBoundGrant finishes an authorization_code grant whose authorization
// named a resource, once the code has been redeemed.
func (app *App) completeBoundGrant(c *echo.Context, pks *storage.PKCEState, req *tokenRequest) error {
	ctx := c.Request().Context()

	resource, err := app.resolveResource(pks.Resource)
	if err != nil || resource == nil {
		return oauthError(c, http.StatusBadRequest, "invalid_target", errInvalidTarget.Error())
	}
	// RFC 8707: the resource of the token request has to be the one that was
	// authorized. Omitting it means "the same".
	if req.Resource != "" {
		named, err := app.resolveResource(req.Resource)
		if err != nil || named.URL != resource.URL {
			return oauthError(c, http.StatusBadRequest, "invalid_target", "resource does not match the authorization request")
		}
	}

	account, err := authservices.GetAccount(pks.UserID, app.mongoClient)
	if err != nil {
		return oauthError(c, http.StatusBadRequest, "invalid_grant", "The account behind this authorization is not available")
	}

	connectionID, err := storage.NewFamilyID()
	if err != nil {
		return oauthError(c, http.StatusInternalServerError, "server_error", "Failed to start the connection")
	}
	scopes := grantedScopes(resource, pks.Scope)
	return app.respondWithBoundTokens(ctx, c, account, resource, scopes, storage.Grant{
		FamilyID:   connectionID,
		ClientID:   pks.ClientID,
		ClientName: app.clientDisplayName(ctx, pks.ClientID),
		UserPrn:    account.Prn,
		Resource:   resource.URL,
	})
}

// clientDisplayName is what a client called itself, for the user's list of
// connections. It is a label and nothing more; what identifies the client is
// its id. A URL client was resolved moments ago during consent, so this comes
// from the cache.
func (app *App) clientDisplayName(ctx context.Context, clientID string) string {
	if cimd.IsClientIDURL(clientID) {
		if document, err := app.urlClient(ctx, clientID); err == nil {
			return document.ClientName
		}
		return ""
	}
	if registered, _, err := apps.SearchApp(ctx, "", clientID, app.mongoClient.Database(utils.MongoDb)); err == nil && registered != nil {
		return registered.Name
	}
	return ""
}

// respondWithBoundTokens writes the token response of a resource-bound grant:
// an access token plus the next refresh token of the connection.
func (app *App) respondWithBoundTokens(ctx context.Context, c *echo.Context, account accounts.Account, resource *OAuthResource, scopes []string, grant storage.Grant) error {
	accessToken, lifetime, err := app.mintBoundAccessToken(account, resource, scopes, grant.ClientID, grant.FamilyID)
	if err != nil {
		return oauthError(c, http.StatusInternalServerError, "server_error", "Error signing new token")
	}

	repo, err := storage.GetRefreshTokenRepo()
	if err != nil {
		return oauthError(c, http.StatusInternalServerError, "server_error", "Refresh tokens are not available")
	}
	grant.Scope = strings.Join(scopes, " ")
	refreshToken, err := repo.Issue(ctx, grant, refreshTokenTTL())
	if err != nil {
		return oauthError(c, http.StatusInternalServerError, "server_error", "Failed to issue a refresh token")
	}

	noStore(c)
	return echoutil.WriteJSON(c, http.StatusOK, authmodels.TokenResponse{
		Token:        accessToken,
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(lifetime.Seconds()),
		RefreshToken: refreshToken,
		Scope:        grant.Scope,
	})
}

// handleRefreshTokenGrant exchanges a refresh token for a new access token and
// the refresh token that replaces it. Every failure is invalid_grant, which is
// what tells a client to stop retrying and send the user through consent again.
func (app *App) handleRefreshTokenGrant(c *echo.Context, req *tokenRequest) error {
	ctx := c.Request().Context()
	invalidGrant := func() error {
		return oauthError(c, http.StatusBadRequest, "invalid_grant", "The refresh token is invalid, expired or revoked")
	}

	if req.RefreshToken == "" || req.ClientID == "" {
		return oauthError(c, http.StatusBadRequest, "invalid_request", "refresh_token and client_id are required")
	}

	repo, err := storage.GetRefreshTokenRepo()
	if err != nil {
		return oauthError(c, http.StatusInternalServerError, "server_error", "Refresh tokens are not available")
	}

	consumed, err := repo.Consume(ctx, req.RefreshToken, req.ClientID)
	if err != nil {
		if errors.Is(err, storage.ErrRefreshTokenInvalid) {
			return invalidGrant()
		}
		return oauthError(c, http.StatusInternalServerError, "server_error", "Failed to read the refresh token")
	}

	// From here on the presented token is spent. Anything that goes wrong has
	// to end the family too, or the client would be left holding nothing while
	// the authorization lives on unusable.
	fail := func() error {
		_ = repo.RevokeFamily(ctx, consumed.FamilyID)
		return invalidGrant()
	}

	resource, err := app.resolveResource(consumed.Resource)
	if err != nil || resource == nil {
		// The resource it was issued for is no longer served here.
		return fail()
	}
	if req.Resource != "" {
		named, err := app.resolveResource(req.Resource)
		if err != nil || named.URL != resource.URL {
			return fail()
		}
	}

	// The account may have been removed or deactivated since consent.
	account, err := authservices.GetAccount(consumed.UserPrn, app.mongoClient)
	if err != nil || account.Prn != consumed.UserPrn || account.Challenge != "" {
		return fail()
	}

	// A refresh can keep or narrow the granted scopes, never widen them. The
	// grant is re-filtered against the resource as well, so a scope withdrawn
	// from the resource disappears from running sessions at their next refresh.
	scopes := grantedScopes(resource, consumed.Scope)
	if req.Scope != "" {
		scopes = narrowScopes(scopes, req.Scope)
		if len(scopes) == 0 {
			_ = repo.RevokeFamily(ctx, consumed.FamilyID)
			return oauthError(c, http.StatusBadRequest, "invalid_scope", "The requested scope exceeds what was granted")
		}
	}

	return app.respondWithBoundTokens(ctx, c, account, resource, scopes, storage.Grant{
		FamilyID:   consumed.FamilyID,
		ClientID:   consumed.ClientID,
		ClientName: consumed.ClientName,
		UserPrn:    consumed.UserPrn,
		Resource:   resource.URL,
		GrantedAt:  consumed.GrantedAt,
	})
}

func narrowScopes(granted []string, requested string) []string {
	wanted := map[string]bool{}
	for _, scope := range utils.ScopeStringFilterBy(strings.Fields(requested), "", "") {
		wanted[scope] = true
	}
	narrowed := []string{}
	for _, scope := range granted {
		if wanted[scope] {
			narrowed = append(narrowed, scope)
		}
	}
	return narrowed
}

// authorizeErrorRedirect reports an authorization error to the client the way
// OAuth specifies, on its redirect URI. It must only be called once that URI
// has been validated for the client: before that, redirecting is exactly what
// an attacker wants, and the error is shown to the user instead.
func authorizeErrorRedirect(c *echo.Context, redirectURI, state, code, description string) error {
	target, err := appendQuery(redirectURI, map[string]string{
		"error":             code,
		"error_description": description,
		"state":             state,
		"iss":               oauthIssuer(),
	})
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, code, description, http.StatusBadRequest)
	}
	http.Redirect(c.Response(), c.Request(), target, http.StatusFound)
	return nil
}

// appendQuery adds params to rawURL, keeping any query it already has and
// skipping empty values.
func appendQuery(rawURL string, params map[string]string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// untrustedClient reports whether clientID is a client nobody here registered:
// a metadata document URL or a dynamically registered application. Such a
// client only ever gets tokens bound to a resource, never an account-wide one.
// It fails closed when the application cannot be looked up.
func (app *App) untrustedClient(ctx context.Context, clientID string) bool {
	if cimd.IsClientIDURL(clientID) {
		return true
	}
	registered, status, err := apps.SearchApp(ctx, "", clientID, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		return status != http.StatusNotFound
	}
	return registered.Dynamic
}

// urlClient resolves the metadata document of a URL client id.
func (app *App) urlClient(ctx context.Context, clientID string) (*cimd.Document, error) {
	if !cimd.Enabled() {
		return nil, ErrUnknownClient
	}
	if app.clientMetadata == nil {
		return nil, ErrUnknownClient
	}
	return app.clientMetadata.Resolve(ctx, clientID)
}

// validateURLClientRedirect pins the redirect target of a URL client to the
// redirect URIs its own metadata document lists, compared exactly.
func (app *App) validateURLClientRedirect(ctx context.Context, clientID, candidate string, audit redirecturi.AuditContext) error {
	document, err := app.urlClient(ctx, clientID)
	if err != nil {
		redirecturi.LogRejection(candidate, err, audit)
		if errors.Is(err, ErrUnknownClient) {
			return ErrUnknownClient
		}
		return errors.New("client_id could not be verified: " + err.Error())
	}
	return redirecturi.ValidateExact(document.RedirectURIs, candidate, audit)
}

// completeURLClientConsent is the consent step of a URL client, reached through
// POST /auth/code like every other consent the web app submits. It reports
// false when the request is not about such a client, leaving it to the
// registered-application path untouched.
//
// A URL client has no application record for that path to check against, so
// this one trusts nothing the browser sends beyond the authorization it names:
// the client, the redirect URI, the state and the scope all come from what was
// stored when the client started the flow, after its redirect URI was checked
// against its metadata document.
func (app *App) completeURLClientConsent(c *echo.Context, caller string, req *codeRequest) (bool, error) {
	ctx := c.Request().Context()

	authCode := req.AuthCode
	if authCode == "" {
		authCode = utils.GetCookie(c.Request(), "pkce_auth_code")
	}
	if authCode == "" {
		return false, nil
	}
	pks, found := pkceservice.GetPKCEState(ctx, authCode)
	if !found || (!cimd.IsClientIDURL(pks.ClientID) && pks.Resource == "") {
		return false, nil
	}

	if req.Service != "" && req.Service != pks.ClientID {
		return true, echoutil.RestErrorWrapperUser(c, "invalid_request", "client_id does not match the authorization request", http.StatusBadRequest)
	}
	if pks.IsUsed || time.Now().After(pks.ExpiresAt) {
		return true, echoutil.RestErrorWrapperUser(c, "invalid_grant", "The authorization request has expired, start again from the application", http.StatusBadRequest)
	}
	if pks.UserID != "" {
		return true, echoutil.RestErrorWrapperUser(c, "invalid_grant", "The authorization request was already approved", http.StatusBadRequest)
	}

	// The redirect URI has to still be one the client stands behind.
	if err := app.validateRedirectURI(ctx, pks.ClientID, pks.RedirectURI, auditContext(c.Request(), "oauth_consent")); err != nil {
		return true, echoutil.RestErrorWrapperUser(c, "invalid_request", err.Error(), http.StatusBadRequest)
	}

	// Whoever started the flow saw the consent handle; the code that goes to
	// the redirect URI is minted only now.
	pks, approved := pkceservice.ApprovePKCEState(ctx, pks.AuthCode, caller)
	if !approved {
		return true, echoutil.RestErrorWrapperUser(c, "invalid_grant", "The authorization request was already approved or has expired", http.StatusBadRequest)
	}

	utils.DeleteCookie(c.Response(), c.Request(), "pkce_redirect_uri")
	utils.DeleteCookie(c.Response(), c.Request(), "pkce_auth_code")

	// iss lets the client tell which authorization server answered (RFC 9207).
	target, err := appendQuery(pks.RedirectURI, map[string]string{
		"code":  pks.AuthCode,
		"state": pks.State,
		"iss":   oauthIssuer(),
	})
	if err != nil {
		return true, echoutil.RestErrorWrapperUser(c, "server_error", "Failed to build the redirect", http.StatusInternalServerError)
	}

	return true, echoutil.WriteJSON(c, http.StatusOK, codeResponse{
		Code:        pks.AuthCode,
		Scopes:      pks.Scope,
		State:       pks.State,
		RedirectURI: target,
	})
}

// oauthClientInfo is what the consent page shows about a client.
type oauthClientInfo struct {
	ClientID     string   `json:"client_id"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	ClientURI    string   `json:"client_uri,omitempty"`
	LogoURI      string   `json:"logo_uri,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`

	// Unverified is set for a client that registered itself: its name and
	// logo are whatever it chose, and only Host is worth showing.
	Unverified bool `json:"unverified,omitempty"`
}

// HandleGetOAuthClient describes a URL client or registered application to the
// consent page. Host is the one field a client cannot make up: the host of a
// URL client id, or for a registered client the host of the redirect_uri the
// consent is for, when the page passes it (else its first redirect URI or nick).
//
// It requires a signed-in user. Note that the authorize endpoint resolves URL
// clients without one; the resolver throttles its fetches for that reason.
func (app *App) HandleGetOAuthClient(c *echo.Context) error {
	ctx := c.Request().Context()
	clientID := c.QueryParam("client_id")
	if clientID == "" {
		return echoutil.RestErrorWrapperUser(c, "invalid_request", "client_id is required", http.StatusBadRequest)
	}

	if cimd.IsClientIDURL(clientID) {
		document, err := app.urlClient(ctx, clientID)
		if err != nil {
			return echoutil.RestErrorWrapperUser(c, "invalid_client", "client_id could not be verified", http.StatusNotFound)
		}

		return echoutil.WriteJSON(c, http.StatusOK, oauthClientInfo{
			ClientID:     document.ClientID,
			Name:         document.ClientName,
			Host:         document.Host(),
			ClientURI:    document.ClientURI,
			LogoURI:      document.LogoURI,
			RedirectURIs: document.RedirectURIs,
			Unverified:   true,
		})
	}

	registered, _, err := apps.SearchApp(ctx, "", clientID, app.mongoClient.Database(utils.MongoDb))
	if err != nil || registered == nil {
		return echoutil.RestErrorWrapperUser(c, "invalid_client", "client_id could not be verified", http.StatusNotFound)
	}

	host := registered.Nick
	redirect := c.QueryParam("redirect_uri")
	if redirect != "" {
		if err := app.validateRedirectURI(ctx, clientID, redirect, auditContext(c.Request(), "oauth_client_info")); err != nil {
			return echoutil.RestErrorWrapperUser(c, "invalid_request", "redirect_uri is not registered for this client", http.StatusBadRequest)
		}
	} else if len(registered.RedirectURIs) > 0 {
		redirect = registered.RedirectURIs[0]
	}
	if u, err := url.Parse(redirect); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}

	return echoutil.WriteJSON(c, http.StatusOK, oauthClientInfo{
		ClientID:     registered.Prn,
		Name:         registered.Name,
		Host:         host,
		LogoURI:      registered.Logo,
		RedirectURIs: registered.RedirectURIs,
		Unverified:   registered.Dynamic,
	})
}

// authorizationServerMetadata is the RFC 8414 document.
type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
	AuthorizationResponseIssSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

// AuthorizationServerMetadataHandler serves the RFC 8414 metadata document,
// which is how a client that only knows the issuer finds everything else. It
// describes the PKCE endpoints only: they are the ones a public client can use.
func (app *App) AuthorizationServerMetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public configuration, read by browser based clients too.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		scopes := map[string]bool{}
		for _, resource := range app.oauthResources {
			for _, scope := range resource.Scopes {
				scopes[scope] = true
			}
		}
		supported := make([]string, 0, len(scopes))
		for scope := range scopes {
			supported = append(supported, scope)
		}
		sort.Strings(supported)

		var registrationEndpoint string
		if app.dcrEnabled() {
			registrationEndpoint = utils.GetAPIEndpoint("/auth/oauth/register")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(authorizationServerMetadata{
			Issuer:                            oauthIssuer(),
			AuthorizationEndpoint:             utils.GetAPIEndpoint("/auth/oauth/authorize"),
			TokenEndpoint:                     utils.GetAPIEndpoint("/auth/oauth/token"),
			RegistrationEndpoint:              registrationEndpoint,
			ResponseTypesSupported:            []string{"code"},
			ResponseModesSupported:            []string{"query"},
			GrantTypesSupported:               []string{grantAuthorizationCode, grantRefreshToken},
			CodeChallengeMethodsSupported:     []string{"S256"},
			TokenEndpointAuthMethodsSupported: []string{"none"},
			ScopesSupported:                   supported,
			ClientIDMetadataDocumentSupported: cimd.Enabled(),
			AuthorizationResponseIssSupported: true,
		})
	})
}

// AuthorizationServerMetadataPaths lists where the metadata document has to be
// reachable. RFC 8414 inserts the well-known segment before the issuer's path,
// so an API served under a version prefix publishes it there as well.
func AuthorizationServerMetadataPaths() []string {
	paths := []string{AuthorizationServerMetadataPath}
	if issuer, err := url.Parse(oauthIssuer()); err == nil {
		if path := strings.Trim(issuer.Path, "/"); path != "" {
			paths = append(paths, AuthorizationServerMetadataPath+"/"+path)
		}
	}
	return paths
}
