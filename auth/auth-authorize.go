// Copyright 2020  Pantacor Ltd.
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

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type codeRequest struct {
	Service      string `json:"service"`
	Scopes       string `json:"scopes"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
	ResponseType string `json:"response_type"`
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

func (app *AuthApp) handlePostAuthorizeToken(w rest.ResponseWriter, r *rest.Request) {
	var err error

	// this is the claim of the service authenticating itself
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)
	callerType := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"].(string)

	if caller == "" {
		rest.Error(w, "must be authenticated as user", http.StatusUnauthorized)
		return
	}

	if callerType != "USER" {
		rest.Error(w, "only USER's can request implicit access tokens", http.StatusForbidden)
		return
	}

	req := implicitTokenRequest{}
	err = r.DecodeJsonPayload(&req)

	if err != nil {
		rest.Error(w, "error decoding token request", http.StatusBadRequest)
		log.Println("WARNING: implicit access token request received with wrong request body: " + err.Error())
		return
	}

	if req.Service == "" {
		rest.Error(w, "implicit  access token requested with invalid service", http.StatusBadRequest)
		return
	}

	errCode, err := app.validateScopesAndURIs("", req.Service, req.Scopes, req.RedirectURI)
	if err != nil {
		rest.Error(w, err.Error(), errCode)
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if app.jwt_middleware.PayloadFunc != nil {
		for key, value := range app.jwt_middleware.PayloadFunc(caller) {
			tokenClaims[key] = value
		}
	}

	tokenClaims["token_id"] = primitive.NewObjectID()
	tokenClaims["id"] = caller
	tokenClaims["aud"] = req.Service
	tokenClaims["scopes"] = req.Scopes
	tokenClaims["prn"] = caller
	tokenClaims["exp"] = time.Now().Add(app.jwt_middleware.Timeout)
	tokenString, err := token.SignedString(app.jwt_middleware.Key)

	if err != nil {
		log.Println("WARNING: error signing implicit access token for service / user / scopes(" + req.Service + " / " + caller + " / " + req.Scopes + ")")
		rest.Error(w, "error signing implicit access token for service / user / scopes("+req.Service+" / "+caller+" / "+req.Scopes+")", http.StatusUnauthorized)
		return
	}

	tokenStore := tokenStore{
		ID:      tokenClaims["token_id"].(primitive.ObjectID),
		Client:  req.Service,
		Owner:   caller,
		Comment: "",
		Claims:  tokenClaims,
	}

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_oauth_accesstokens")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	// XXX: prototype: for production we need to prevent posting twice!!
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		tokenStore,
	)
	if err != nil {
		rest.Error(w, "Error inserting oauth token into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	params := url.Values{}
	params.Add("token_type", "bearer")
	params.Add("access_token", tokenString)
	params.Add("expires_in", fmt.Sprintf("%d", app.jwt_middleware.Timeout/time.Second))
	params.Add("scope", req.Scopes)
	params.Add("state", req.State)

	response := tokenResponse{
		Token:       tokenString,
		RedirectURI: req.RedirectURI + "#" + params.Encode(),
		TokenType:   "bearer",
		Scopes:      req.Scopes,
	}

	w.WriteJson(response)
}

func (app *AuthApp) handlePostCode(w rest.ResponseWriter, r *rest.Request) {
	var err error

	// this is the claim of the service authenticating itself
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)
	callerType := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"].(string)

	if caller == "" {
		rest.Error(w, "must be authenticated as user", http.StatusUnauthorized)
		return
	}

	if callerType != "USER" {
		rest.Error(w, "only USER's can request access codes", http.StatusForbidden)
		return
	}

	req := codeRequest{}
	err = r.DecodeJsonPayload(&req)

	errCode, err := app.validateScopesAndURIs("", req.Service, req.Scopes, req.RedirectURI)
	if err != nil {
		rest.Error(w, err.Error(), errCode)
		return
	}

	var mapClaim jwtgo.MapClaims
	mapClaim = app.accessCodePayload(caller, req.Service, req.Scopes)
	if mapClaim == nil {
		userAccountPayload := app.getAccountPayload(caller)
		mapClaim, err = apps.AccessCodePayload(
			"",
			req.Service,
			req.ResponseType,
			req.Scopes,
			userAccountPayload,
			app.mongoClient.Database(utils.MongoDb))
		if err != nil {
			utils.RestError(w, nil, err.Error(), http.StatusBadRequest)
			return
		}
	}

	mapClaim["exp"] = time.Now().Add(time.Minute * 5)

	response := codeResponse{}
	code := jwtgo.New(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm))
	code.Claims = mapClaim

	response.Code, err = code.SignedString(app.jwt_middleware.Key)
	response.Scopes = req.Scopes

	params := url.Values{}
	params.Add("code", response.Code)
	params.Add("state", req.State)
	response.RedirectURI = req.RedirectURI + "?" + params.Encode()
	w.WriteJson(response)
}

func containsStringWithPrefix(slice []string, prefix string) bool {
	for _, v := range slice {
		if strings.HasPrefix(prefix, v) {
			return true
		}
	}
	return false
}

func (app *AuthApp) validateScopesAndURIs(caller, reqService, reqScopes, reqRedirectURI string) (int, error) {
	defaultAccount := false
	service, _, err := apps.SearchApp(caller, reqService, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		// Support default accounts as before but only use pantahub scopes for those
		serviceAccount, err := app.getAccount(reqService)
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
	if defaultAccount == false {
		scopes := strings.Fields(reqScopes)
		allServicesScopes := utils.ParseScopes(service.Scopes)
		if !utils.MatchAllScope(scopes, allServicesScopes) {
			return http.StatusBadRequest, errors.New("You use a not allowed scoped for this application")
		}
	}

	if reqRedirectURI != "" {
		if service.RedirectURIs != nil && !containsStringWithPrefix(service.RedirectURIs, reqRedirectURI) {
			return http.StatusBadRequest, errors.New("error implicit access token failed; redirect URL does not match registered service")
		}
	}

	return 0, nil
}
