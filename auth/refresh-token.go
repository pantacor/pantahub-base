// Copyright (c) 2017-2026 Pantacor Ltd.
//

package auth

import (
	"errors"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"log"
	"net/http"
	"strconv"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// handlePostTokenRefresh refreshes a user-on-behalf access token. The caller
// authenticates as the service the token was issued for; the request body
// carries the existing access token. The response is a new access token with
// the same identity (prn / id / nick / roles / scopes) but fresh exp and
// orig_iat. Signature is verified, but exp is intentionally NOT validated so
// that recently-expired tokens can still be rotated by an active service.
//
// @Summary Refresh a service-issued user access token
// @Description Refresh a token previously obtained via POST /auth/token.
// @Description The caller must authenticate as the service that owns the token
// @Description (i.e. its PRN must equal the token's "aud" claim).
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param body body authmodels.TokenRefreshRequest true "Token to refresh"
// @Success 200 {object} authmodels.TokenResponse
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 401 {object} utils.RError "Unauthorized"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/token/refresh [post]
func (a *App) handlePostTokenRefresh(c *echo.Context) error {
	req := authmodels.TokenRefreshRequest{}
	if err := echoutil.DecodeJsonPayload(c, &req); err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode refresh request", http.StatusBadRequest)
	}
	if req.Token == "" {
		return echoutil.RestErrorWrapper(c, "Missing token in refresh request", http.StatusBadRequest)
	}

	callerClaims, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Caller has no JWT payload", http.StatusUnauthorized)
	}
	caller, _ := callerClaims["prn"].(string)
	if caller == "" {
		return echoutil.RestErrorWrapper(c, "Caller has no prn", http.StatusUnauthorized)
	}
	// Only service identities can refresh service-issued tokens. The token
	// minted by /auth/token carries the user's identity (type=USER) but its
	// "aud" is the service PRN; refreshing must therefore be initiated by
	// that service authenticating itself, not by reusing the user-on-behalf
	// bearer.
	callerType, _ := callerClaims["type"].(string)
	if callerType != string(accounts.AccountTypeService) {
		log.Printf("WARNING: non-service caller %q (type=%q) tried to refresh a service token", caller, callerType)
		return echoutil.RestErrorWrapper(c, "Only service callers may refresh service tokens", http.StatusForbidden)
	}

	// Verify the signature but skip exp/nbf/iat, so a recently expired service token
	// can still be refreshed.
	parser := jwtgo.NewParser(jwtgo.WithoutClaimsValidation())
	tok, err := parser.Parse(req.Token, func(t *jwtgo.Token) (interface{}, error) {
		if jwtgo.GetSigningMethod(a.jwtConfig.SigningAlgorithm) != t.Method {
			return nil, errors.New("invalid signing algorithm")
		}
		return a.jwtConfig.Pub, nil
	})
	if err != nil || tok == nil || !tok.Valid {
		log.Println("DEBUG: refresh-token parse error:", err)
		return echoutil.RestErrorWrapper(c, "Invalid token", http.StatusUnauthorized)
	}

	oldClaims, ok := tok.Claims.(jwtgo.MapClaims)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Invalid token claims", http.StatusUnauthorized)
	}

	aud, _ := oldClaims["aud"].(string)
	if aud == "" {
		return echoutil.RestErrorWrapper(c, "Token has no aud — not refreshable", http.StatusUnauthorized)
	}
	if aud != caller {
		log.Printf("WARNING: caller %q tried to refresh a token issued for %q", caller, aud)
		return echoutil.RestErrorWrapper(c, "Caller is not the audience of this token", http.StatusUnauthorized)
	}

	// The subject must still exist and be active: a token for a deleted or
	// never-activated account must not be renewable indefinitely.
	subject, _ := oldClaims["prn"].(string)
	if subject == "" {
		return echoutil.RestErrorWrapper(c, "Token has no subject — not refreshable", http.StatusUnauthorized)
	}
	account, err := authservices.GetAccount(subject, a.mongoClient)
	if err != nil || account.Prn != subject || account.Challenge != "" {
		log.Printf("WARNING: refusing to refresh token for inactive or missing account %q", subject)
		return echoutil.RestErrorWrapper(c, "Account is not active", http.StatusUnauthorized)
	}

	// Mint a new token preserving identity claims.
	newToken := jwtgo.New(jwtgo.GetSigningMethod(a.jwtConfig.SigningAlgorithm))
	newClaims := newToken.Claims.(jwtgo.MapClaims)
	for k, v := range oldClaims {
		newClaims[k] = v
	}

	timeoutStr := utils.GetEnv(utils.EnvPantahubAuthorizeJWTTimeoutMinutes)
	authorizeTimeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		authorizeTimeout = 1920
	}
	now := time.Now()
	newClaims["exp"] = now.Add(time.Minute * time.Duration(authorizeTimeout)).Unix()
	newClaims["orig_iat"] = now.Unix()
	newClaims["token_id"] = primitive.NewObjectID()

	tokenString, err := newToken.SignedString(a.jwtConfig.Key)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error signing refreshed token", http.StatusInternalServerError)
	}

	scopes, _ := newClaims["scopes"].(string)
	return echoutil.WriteJSON(c, http.StatusOK, authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    scopes,
	})
}
