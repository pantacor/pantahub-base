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

package authmodels

import (
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AccountType Defines the type of account
type AccountType string

type TokenResponse struct {
	Token       string `json:"token"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	State       string `json:"state,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
	Scopes      string `json:"scopes,omitempty"`
	ExpiresIn   int    `json:"expires_in,omitempty"`

	// AccessToken repeats Token under the name OAuth gives it (RFC 6749
	// section 5.1). Token stays because the clients written against this API
	// read that; standard OAuth clients only look for access_token.
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type PasswordResetRequest struct {
	Email string `json:"email"`
}

type EncryptedAccountToken struct {
	Token       string `json:"token"`
	RedirectURI string `json:"redirect-uri"`
}

type AccountCreationPayload struct {
	accounts.Account
	Captcha          string `json:"captcha"`
	EncryptedAccount string `json:"encrypted-account"`
}

type PasswordReset struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type ResetPasswordClaims struct {
	Email        string    `json:"email"`
	TimeModified time.Time `json:"time-modified"`
	jwtgo.RegisteredClaims
}

// this requests to swap access code with accesstoken
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
	Code         string `json:"access-code"`
	Comment      string `json:"comment"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

type TokenStore struct {
	ID      primitive.ObjectID     `json:"id" bson:"_id"`
	Client  string                 `json:"client"`
	Owner   string                 `json:"owner"`
	Comment string                 `json:"comment"`
	Claims  map[string]interface{} `json:"jwt-claims"`
}

// TokenRefreshRequest is sent by a service to refresh a user-on-behalf access
// token it previously received via POST /auth/token. The caller must
// authenticate as the service whose PRN matches the "aud" claim of Token.
type TokenRefreshRequest struct {
	Token string `json:"token"`
}

type LoginRequestPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Scope    string `json:"scope"`
}
