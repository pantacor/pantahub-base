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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/mfaservice"
	"gitlab.com/pantacor/pantahub-base/auth/oauth"
	"gitlab.com/pantacor/pantahub-base/auth/pkceservice"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo"
)

const DubKeyErrCode = 11000

// TokenPayload login token payload
type TokenPayload struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type,omitempty"`
	Scopes    string `json:"scopes,omitempty"`
}

// HandleGetThirdPartyLogin login or register user using thirdparty integration
// @Summary login or register user using thirdparty integration
// @Description login or register user using thirdparty integration
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param service path string false "External oAuth service"
// @Param returnto query string false "Return to with implicit token"
// @Redirect 303
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 403 {object} utils.RError "user has no admin role"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/oauth/login/{service} [get]
func (a *App) HandleGetThirdPartyLogin(c *echo.Context) error {
	audit := auditContext(c.Request(), "social_login")
	audit.Service = c.Param("service")

	return oauth.AuthorizeByService(c, func(redirectURI string) error {
		return validateSocialRedirectURI(redirectURI, audit)
	})
}

// HandleGetThirdPartyCallback login or register user using thirdparty integration
// @Summary login or register user using thirdparty integration
// @Description login or register user using thirdparty integration
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param service path string false "External oAuth service"
// @Param returnto query string false "Return to with implicit token"
// @Success 200 {object} TokenPayload
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 403 {object} utils.RError "user has no admin role"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/oauth/callback/{service} [get]
func (a *App) HandleGetThirdPartyCallback(c *echo.Context) error {
	payload, err := oauth.CbByService(c)
	if err != nil {
		redirectTo := ""
		if payload != nil {
			redirectTo = payload.RedirectTo
		}
		return processErr(c, err, "Unable to connect to thirdparty service", http.StatusForbidden, redirectTo)
	}
	if payload == nil {
		return processErr(c, fmt.Errorf("empty OAuth provider response"), "Unable to connect to thirdparty service", http.StatusForbidden, "")
	}

	// The return target is carried inside the signed state, so it cannot have
	// been tampered with since we issued it. It is checked again here so that a
	// target that was allowed at authorize time but is no longer configured
	// cannot receive a token.
	if payload.RedirectTo != "" {
		audit := auditContext(c.Request(), "social_login_callback")
		audit.Service = string(payload.Service)

		if err := validateSocialRedirectURI(payload.RedirectTo, audit); err != nil {
			payload.RedirectTo = ""
			return echoutil.RestError(c, err, err.Error(), http.StatusBadRequest)
		}
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if payload.ProviderID == "" {
		return processErr(c, fmt.Errorf("OAuth provider ID is missing"), "Unable to identify OAuth account", http.StatusForbidden, payload.RedirectTo)
	}
	if payload.Email != "" && !authservices.IsEmailDomainAllowed(payload.Email) {
		errMg := fmt.Sprintf("Email domain not allowed: %s", payload.Email)
		return processErr(c, fmt.Errorf("email domain is not allowed"), errMg, http.StatusForbidden, payload.RedirectTo)
	}

	// A connect flow carries the authenticated account PRN inside the signed
	// OAuth state (see AuthorizationURLByServiceWithConnect), not a cross-site
	// cookie. The state signature means the callback trusts only a PRN we signed
	// when the authenticated request started the flow; the browser cannot supply
	// one of its own.
	if payload.ConnectPRN != "" {
		account, err := getUserByPRN(c.Request().Context(), payload.ConnectPRN, collection)
		if err != nil {
			return processErr(c, err, "Account not found", http.StatusForbidden, payload.RedirectTo)
		}
		provider := accounts.ConnectedProvider{
			Service:     string(payload.Service),
			ProviderID:  payload.ProviderID,
			Email:       payload.Email,
			ConnectedAt: time.Now(),
		}
		if err := connectProvider(c.Request().Context(), account.Prn, provider, collection); err != nil {
			status := http.StatusInternalServerError
			if isDubplicateKey("connected_providers", err) {
				status = http.StatusConflict
			}
			return processErr(c, err, "OAuth provider is already connected to another account", status, payload.RedirectTo)
		}
		return redirectAfterProviderConnect(c, payload.RedirectTo, provider)
	}

	// Login is resolved by service + stable provider ID. Email is only used to
	// provision a brand-new OAuth account; it is never sufficient to sign in to
	// an existing account.
	account, err := getUserByProvider(c.Request().Context(), string(payload.Service), payload.ProviderID, collection)
	if err != nil && err != mongo.ErrNoDocuments {
		return processErr(c, err, "Error with Database connectivity", http.StatusInternalServerError, payload.RedirectTo)
	}
	if err == mongo.ErrNoDocuments {
		if payload.Email == "" {
			errMg := fmt.Sprintf("You need to validate your email or make it public on %s", payload.Service)
			return processErr(c, fmt.Errorf("email is missing"), errMg, http.StatusForbidden, payload.RedirectTo)
		}

		account, err = getUserByEmail(c.Request().Context(), payload.Email, collection)
		if err != nil && err != mongo.ErrNoDocuments {
			return processErr(c, err, "Error with Database connectivity", http.StatusInternalServerError, payload.RedirectTo)
		}
		if err == mongo.ErrNoDocuments {
			account, err = createUser(c.Request().Context(), payload.Email, payload.Nick, "", "", collection)
			if err != nil && isDubplicateKey("nick", err) {
				scopeNick := payload.Nick + "_" + string(payload.Service)
				account, err = createUser(c.Request().Context(), payload.Email, scopeNick, "", "", collection)
			}
			if err == nil {
				provider := accounts.ConnectedProvider{
					Service:     string(payload.Service),
					ProviderID:  payload.ProviderID,
					Email:       payload.Email,
					ConnectedAt: time.Now(),
				}
				err = connectProvider(c.Request().Context(), account.Prn, provider, collection)
			}

			if err == nil {
				urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://" + utils.GetEnv(utils.EnvPantahubWWWHost)
				if utils.GetEnv(utils.EnvPantahubPort) != "" {
					urlPrefix += ":"
					urlPrefix += utils.GetEnv(utils.EnvPantahubPort)
				}
				if err := utils.SendWelcome(account.Email, account.Nick, urlPrefix); err != nil {
					log.Printf("WARNING: sending welcome mail to the new account failed: %v", err)
				}
			}
		} else if connectedAccountsEnforced() {
			return processErr(c, fmt.Errorf("OAuth provider is not connected to this account"), "This OAuth account is not connected; sign in with your password and connect it first", http.StatusForbidden, payload.RedirectTo)
		} else {
			// Legacy opt-out mode keeps email-based social login working, but
			// records the stable identity for subsequent logins.
			provider := accounts.ConnectedProvider{
				Service:     string(payload.Service),
				ProviderID:  payload.ProviderID,
				Email:       payload.Email,
				ConnectedAt: time.Now(),
			}
			err = connectProvider(c.Request().Context(), account.Prn, provider, collection)
		}
	}
	if err != nil {
		return processErr(c, err, "Error with Database connectivity", http.StatusInternalServerError, payload.RedirectTo)
	}

	pkceAuthCode := utils.GetCookie(c.Request(), "pkce_auth_code")
	pkceRedirectURI := utils.GetCookie(c.Request(), "pkce_redirect_uri")
	if pkceRedirectURI != "" && pkceAuthCode != "" && isValidCallbackURL(pkceRedirectURI) {
		utils.DeleteCookie(c.Response(), c.Request(), "pkce_redirect_uri")
		utils.DeleteCookie(c.Response(), c.Request(), "pkce_auth_code")

		pks, found := pkceservice.GetPKCEState(c.Request().Context(), pkceAuthCode)
		if found {
			pkceservice.UpdatePKCEStateUserID(c.Request().Context(), pks.AuthCode, account.Prn)
		}
	}

	// social logins step up to the second factor like password logins do;
	// the challenge is carried to the login page via the redirect fragment
	if handled := a.maybeStartSocialMFALogin(c, account, payload.RedirectTo); handled {
		return nil
	}

	token, err := createAccountToken(account)
	if err != nil {
		return processErr(c, err, err.Error(), http.StatusInternalServerError, payload.RedirectTo)
	}

	if payload.RedirectTo != "" {
		redirectURI := fmt.Sprintf("%s#token=%s", payload.RedirectTo, url.QueryEscape(token.Token))
		http.Redirect(c.Response(), c.Request(), redirectURI, http.StatusTemporaryRedirect)
		return nil
	}

	return echoutil.WriteJSON(c, http.StatusOK, token)
}

func connectedAccountsEnforced() bool {
	return strings.ToLower(strings.TrimSpace(utils.GetEnv(utils.EnvPantahubOAuthConnectedAccountsEnforce))) != "false"
}

// maybeStartSocialMFALogin issues the MFA challenge for social logins into
// accounts that have two-factor authentication enabled. Returns handled ==
// true when a response was already written.
func (a *App) maybeStartSocialMFALogin(c *echo.Context, account *accounts.Account, redirectTo string) bool {
	if !mfaFeatureEnabled() || a.mfaRepo == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	settings, err := a.mfaRepo.GetByOwner(ctx, account.Prn)
	if err != nil {
		// fail closed: never fall through to a single-factor social session
		// for an account that may be MFA-protected when the state is unknown
		_ = processErr(c, err, "Please try again later", http.StatusServiceUnavailable, redirectTo)
		return true
	}
	if settings == nil || !settings.Enabled {
		return false
	}

	methods := a.availableMFAMethods(ctx, settings)

	mfaToken, err := mfaservice.CreateMFAPendingToken(
		a.jwtConfig,
		account.Nick,
		account.Prn,
		"",
		[]string{"oauth"},
		methods,
	)
	if err != nil {
		_ = processErr(c, err, "Error creating MFA token", http.StatusInternalServerError, redirectTo)
		return true
	}

	if redirectTo != "" {
		redirectURI := fmt.Sprintf("%s#mfa_token=%s&mfa_methods=%s",
			redirectTo,
			url.QueryEscape(mfaToken),
			url.QueryEscape(strings.Join(methods, ",")),
		)
		http.Redirect(c.Response(), c.Request(), redirectURI, http.StatusTemporaryRedirect)
		return true
	}

	noStore(c)
	_ = echoutil.WriteJSON(c, http.StatusOK, authmodels.MFARequiredResponse{
		MFARequired: true,
		MFAToken:    mfaToken,
		Methods:     methods,
	})
	return true
}

// processErr reports an error to the caller, bouncing back to the return target
// when there is one. redirectTo must already have been validated: it is only
// ever the value carried in the signed state.
func processErr(c *echo.Context, err error, msg string, code int, redirectTo string) error {
	if redirectTo != "" {
		redirectURI := fmt.Sprintf("%s?error=%s", redirectTo, url.QueryEscape(msg))
		http.Redirect(c.Response(), c.Request(), redirectURI, http.StatusTemporaryRedirect)
		return nil
	}

	return echoutil.RestError(c, err, msg, code)
}

func createAccountToken(account *accounts.Account) (*TokenPayload, error) {
	token := jwt.New(jwt.GetSigningMethod("RS256"))
	claims := token.Claims.(jwt.MapClaims)

	timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		return nil, err
	}
	jwtSecretBase64 := utils.GetEnv(utils.EnvPantahubJWTAuthSecret)
	jwtSecretPem, err := base64.StdEncoding.DecodeString(jwtSecretBase64)
	if err != nil {
		return nil, fmt.Errorf("No valid JWT secret (PANTAHUB_JWT_SECRET) in base64 format: %s", err.Error())
	}
	jwtSecret, err := jwt.ParseRSAPrivateKeyFromPEM(jwtSecretPem)
	if err != nil {
		return nil, err
	}
	claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	claims["id"] = account.Prn
	claims["nick"] = account.Nick
	claims["prn"] = account.Prn
	claims["roles"] = "user"
	claims["type"] = "USER"
	claims["scopes"] = "prn:pantahub.com:apis:/base/all"
	claims["orig_iat"] = time.Now().Unix()

	tokenString, err := token.SignedString(jwtSecret)

	return &TokenPayload{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    "prn:pantahub.com:apis:/base/all",
	}, err
}

func isDubplicateKey(key string, err error) bool {
	return strings.Contains(err.Error(), "duplicate key error collection") &&
		strings.Contains(err.Error(), "index: "+key)
}

// isValidCallbackURL checks if the provided URL is a valid localhost callback URL.
func isValidCallbackURL(callbackURL string) bool {
	u, err := url.Parse(callbackURL)
	if err != nil {
		return false
	}

	// Only allow http or https schemes
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Only allow localhost or 127.0.0.1 as hostname for security
	if u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return false
	}

	// Ensure there's a path, typically /callback
	if !strings.HasPrefix(u.Path, "/callback") {
		return false
	}

	return true
}
