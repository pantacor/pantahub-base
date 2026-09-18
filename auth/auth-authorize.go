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
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/pkceservice"
	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type codeRequest struct {
	Service      string `json:"service"`
	Scopes       string `json:"scopes"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
	ResponseType string `json:"response_type"`
	AuthCode     string `json:"auth_code"`
}

type codeResponse struct {
	Code        string `json:"code"`
	Scopes      string `json:"scopes,omitempty"`
	State       string `json:"state,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}
type implicitTokenRequest struct {
	codeRequest
	RedirectURI string `json:"redirect_uri"`
}

// handlePostAuthorizeToken authorize a thridparty application using OAuth 2.0
// @Summary authorize a thridparty application using OAuth 2.0
// @Description authorize a thridparty application using OAuth 2.0
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param client_id query string false "OAuth Client ID"
// @Param scope query string false "List of required scopes"
// @Param redirect_uri query string false "URL for redirection when process finished"
// @Param response_type query string false "Type of response could be "code|token""
// @Success 302
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/authorize [post]
func (app *App) handlePostAuthorizeToken(c *echo.Context) error {
	var err error

	// this is the claim of the service authenticating itself
	caller := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"].(string)
	if caller == "" {
		return echoutil.RestErrorWrapper(c, "must be authenticated as user", http.StatusUnauthorized)
	}

	req := implicitTokenRequest{}
	err = echoutil.DecodeJsonPayload(c, &req)

	if err != nil {
		log.Println("WARNING: implicit access token request received with wrong request body: " + err.Error())
		return echoutil.RestErrorWrapper(c, "error decoding token request", http.StatusBadRequest)
	}

	if req.Service == "" {
		return echoutil.RestErrorWrapper(c, "implicit  access token requested with invalid service", http.StatusBadRequest)
	}

	errCode, err := app.validateScopesAndURIs(c.Request().Context(), "", req.Service, req.Scopes, req.RedirectURI, auditContext(c.Request(), "implicit_token"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), errCode)
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwtConfig.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if app.jwtConfig.PayloadFunc != nil {
		for key, value := range app.jwtConfig.PayloadFunc(caller) {
			tokenClaims[key] = value
		}
	}

	tokenClaims["token_id"] = primitive.NewObjectID()
	tokenClaims["id"] = caller
	tokenClaims["aud"] = req.Service
	tokenClaims["scopes"] = req.Scopes
	tokenClaims["prn"] = caller
	tokenClaims["orig_iat"] = time.Now().Unix()
	tokenClaims["exp"] = time.Now().Add(app.jwtConfig.Timeout).Unix()
	tokenString, err := token.SignedString(app.jwtConfig.Key)

	if err != nil {
		log.Println("WARNING: error signing implicit access token for service / user / scopes(" + req.Service + " / " + caller + " / " + req.Scopes + ")")
		return echoutil.RestErrorWrapper(c, "error signing implicit access token for service / user / scopes("+req.Service+" / "+caller+" / "+req.Scopes+")", http.StatusUnauthorized)
	}

	tokenStore := authmodels.TokenStore{
		ID:      tokenClaims["token_id"].(primitive.ObjectID),
		Client:  req.Service,
		Owner:   caller,
		Comment: "",
		Claims:  tokenClaims,
	}

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_oauth_accesstokens")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}
	// XXX: prototype: for production we need to prevent posting twice!!
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		tokenStore,
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error inserting oauth token into database "+err.Error(), http.StatusInternalServerError)
	}

	params := url.Values{}
	params.Add("token_type", "bearer")
	params.Add("access_token", tokenString)
	params.Add("expires_in", fmt.Sprintf("%d", app.jwtConfig.Timeout/time.Second))
	params.Add("scope", req.Scopes)
	params.Add("state", req.State)

	pkceAuthCode := utils.GetCookie(c.Request(), "pkce_auth_code")
	pkceRedirectURI := utils.GetCookie(c.Request(), "pkce_redirect_uri")

	if req.AuthCode != "" {
		pkceAuthCode = req.AuthCode
		pkceRedirectURI = req.RedirectURI
	}

	if pkceRedirectURI != "" && pkceAuthCode != "" && isValidCallbackURL(pkceRedirectURI) {
		utils.DeleteCookie(c.Response(), c.Request(), "pkce_redirect_uri")
		utils.DeleteCookie(c.Response(), c.Request(), "pkce_auth_code")

		pks, found := pkceservice.GetPKCEState(c.Request().Context(), pkceAuthCode)
		if found {
			pkceservice.UpdatePKCEStateUserID(c.Request().Context(), pks.AuthCode, caller)
		}
	}

	response := authmodels.TokenResponse{
		Token:       tokenString,
		RedirectURI: req.RedirectURI + "#" + params.Encode(),
		TokenType:   "bearer",
		Scopes:      req.Scopes,
	}

	return echoutil.WriteJSON(c, http.StatusOK, response)
}

// handlePostCode Gets authentication code using OAuth 2.0
// @Summary Gets authentication code using OAuth 2.0
// @Description Gets authentication code using OAuth 2.0
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param client_id query string false "OAuth Client ID"
// @Param scope query string false "List of required scopes"
// @Param redirect_uri query string false "URL for redirection when process finished"
// @Param response_type query string false "Type of response could be "code|token""
// @Success 302
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/code [post]
func (app *App) handlePostCode(c *echo.Context) error {
	var err error

	// this is the claim of the service authenticating itself
	caller := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"].(string)
	callerType := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"].(string)

	if caller == "" {
		return echoutil.RestErrorWrapper(c, "must be authenticated as user", http.StatusUnauthorized)
	}

	if callerType != "USER" {
		return echoutil.RestErrorWrapper(c, "only USER's can request access codes", http.StatusForbidden)
	}

	req := codeRequest{}
	err = echoutil.DecodeJsonPayload(c, &req)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
	}
	errCode, err := app.validateScopesAndURIs(c.Request().Context(), "", req.Service, req.Scopes, req.RedirectURI, auditContext(c.Request(), "authorization_code"))
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), errCode)
	}

	var mapClaim jwtgo.MapClaims
	mapClaim = app.accessCodePayload(caller, req.Service, req.Scopes)
	if mapClaim == nil {
		userAccountPayload := app.getAccountPayload(caller)
		mapClaim, err = apps.AccessCodePayload(
			c.Request().Context(),
			"",
			req.Service,
			req.ResponseType,
			req.Scopes,
			userAccountPayload,
			app.mongoClient.Database(utils.MongoDb))
		if err != nil {
			return echoutil.RestError(c, nil, err.Error(), http.StatusBadRequest)
		}
	}

	mapClaim["exp"] = time.Now().Add(time.Minute * 5).Unix()

	response := codeResponse{}
	code := jwtgo.New(jwtgo.GetSigningMethod(app.jwtConfig.SigningAlgorithm))
	code.Claims = mapClaim

	response.Code, err = code.SignedString(app.jwtConfig.Key)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
	}
	response.Scopes = req.Scopes

	pkceAuthCode := utils.GetCookie(c.Request(), "pkce_auth_code")
	pkceRedirectURI := utils.GetCookie(c.Request(), "pkce_redirect_uri")

	if req.AuthCode != "" {
		pkceAuthCode = req.AuthCode
		pkceRedirectURI = req.RedirectURI
	}

	if pkceRedirectURI != "" && pkceAuthCode != "" && isValidCallbackURL(pkceRedirectURI) {
		utils.DeleteCookie(c.Response(), c.Request(), "pkce_redirect_uri")
		utils.DeleteCookie(c.Response(), c.Request(), "pkce_auth_code")

		pks, found := pkceservice.GetPKCEState(c.Request().Context(), pkceAuthCode)
		if found {
			pkceservice.UpdatePKCEStateUserID(c.Request().Context(), pks.AuthCode, caller)
			response.Code = pks.AuthCode
		}
	}

	params := url.Values{}
	params.Add("code", response.Code)
	params.Add("state", req.State)
	response.RedirectURI = req.RedirectURI + "?" + params.Encode()
	return echoutil.WriteJSON(c, http.StatusOK, response)
}

func (app *App) validateScopesAndURIs(ctx context.Context, caller, reqService, reqScopes, reqRedirectURI string, audit redirecturi.AuditContext) (int, error) {
	defaultAccount := false
	service, _, err := apps.SearchApp(ctx, caller, reqService, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		// Support default accounts as before but only use pantahub scopes for those
		serviceAccount, err := authservices.GetAccount(reqService, app.mongoClient)
		if err != nil && err != mongo.ErrNoDocuments {
			log.Println("error implicit access token creation failed to look up service: " + err.Error())
			return http.StatusInternalServerError, errors.New("error  implicit access token creation failed to look up service")
		}

		if err != nil && err == mongo.ErrNoDocuments {
			return http.StatusBadRequest, errors.New("error access token failed, due to unknown service id")
		}

		service = new(apps.TPApp)
		service.Prn = serviceAccount.Prn
		service.Scopes = utils.PhScopeArray
		service.RedirectURIs = serviceAccount.Oauth2RedirectURIs
		defaultAccount = true
	}

	// Validate scope only when the app comes from database
	if !defaultAccount {
		scopes := strings.Fields(reqScopes)
		allServicesScopes := utils.MarshalScopes(service.Scopes)
		if !utils.MatchAllScope(scopes, allServicesScopes) {
			return http.StatusBadRequest, errors.New("you use a not allowed scoped for this application")
		}
	}

	// The redirect target must sit on a callback URL registered on the service.
	// An app with no registered callbacks fails closed rather than skipping the
	// check.
	if reqRedirectURI != "" {
		audit.ClientID = reqService
		if err := redirecturi.Validate(service.RedirectURIs, reqRedirectURI, audit); err != nil {
			return http.StatusBadRequest, err
		}
	}

	return 0, nil
}
