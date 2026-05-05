package authmodels

import (
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
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
	jwtgo.StandardClaims
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
