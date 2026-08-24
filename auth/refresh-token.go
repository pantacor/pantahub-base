// Copyright 2026 Pantacor Ltd.
//

package auth

import (
	"errors"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
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
func (a *App) handlePostTokenRefresh(writer rest.ResponseWriter, r *rest.Request) {
	req := authmodels.TokenRefreshRequest{}
	if err := r.DecodeJsonPayload(&req); err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode refresh request", http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		utils.RestErrorWrapper(writer, "Missing token in refresh request", http.StatusBadRequest)
		return
	}

	callerClaims, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)
	if !ok {
		utils.RestErrorWrapper(writer, "Caller has no JWT payload", http.StatusUnauthorized)
		return
	}
	caller, _ := callerClaims["prn"].(string)
	if caller == "" {
		utils.RestErrorWrapper(writer, "Caller has no prn", http.StatusUnauthorized)
		return
	}
	// Only service identities can refresh service-issued tokens. The token
	// minted by /auth/token carries the user's identity (type=USER) but its
	// "aud" is the service PRN; refreshing must therefore be initiated by
	// that service authenticating itself, not by reusing the user-on-behalf
	// bearer.
	callerType, _ := callerClaims["type"].(string)
	if callerType != string(accounts.AccountTypeService) {
		log.Printf("WARNING: non-service caller %q (type=%q) tried to refresh a service token", caller, callerType)
		utils.RestErrorWrapper(writer, "Only service callers may refresh service tokens", http.StatusForbidden)
		return
	}

	// Parse with signature verification but skip claim (exp/nbf/iat)
	// validation so that a recently-expired token can still be refreshed by
	// an active service.
	parser := &jwtgo.Parser{SkipClaimsValidation: true}
	tok, err := parser.Parse(req.Token, func(t *jwtgo.Token) (interface{}, error) {
		if jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm) != t.Method {
			return nil, errors.New("invalid signing algorithm")
		}
		return a.jwtMiddleware.Pub, nil
	})
	if err != nil || tok == nil || !tok.Valid {
		log.Println("DEBUG: refresh-token parse error:", err)
		utils.RestErrorWrapper(writer, "Invalid token", http.StatusUnauthorized)
		return
	}

	oldClaims, ok := tok.Claims.(jwtgo.MapClaims)
	if !ok {
		utils.RestErrorWrapper(writer, "Invalid token claims", http.StatusUnauthorized)
		return
	}

	aud, _ := oldClaims["aud"].(string)
	if aud == "" {
		utils.RestErrorWrapper(writer, "Token has no aud — not refreshable", http.StatusUnauthorized)
		return
	}
	if aud != caller {
		log.Printf("WARNING: caller %q tried to refresh a token issued for %q", caller, aud)
		utils.RestErrorWrapper(writer, "Caller is not the audience of this token", http.StatusUnauthorized)
		return
	}

	// The subject must still exist and be active: a token for a deleted or
	// never-activated account must not be renewable indefinitely.
	subject, _ := oldClaims["prn"].(string)
	if subject == "" {
		utils.RestErrorWrapper(writer, "Token has no subject — not refreshable", http.StatusUnauthorized)
		return
	}
	account, err := authservices.GetAccount(subject, a.mongoClient)
	if err != nil || account.Prn != subject || account.Challenge != "" {
		log.Printf("WARNING: refusing to refresh token for inactive or missing account %q", subject)
		utils.RestErrorWrapper(writer, "Account is not active", http.StatusUnauthorized)
		return
	}

	// Mint a new token preserving identity claims.
	newToken := jwtgo.New(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm))
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

	tokenString, err := newToken.SignedString(a.jwtMiddleware.Key)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error signing refreshed token", http.StatusInternalServerError)
		return
	}

	scopes, _ := newClaims["scopes"].(string)
	writer.WriteJson(authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    scopes,
	})
}
