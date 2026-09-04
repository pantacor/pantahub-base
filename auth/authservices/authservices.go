package authservices

import (
	"context"
	"encoding/base64"
	"errors"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenmodels"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenrepo"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenservice"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

func CreateAnonToken(jwtMiddleware *jwt.JWTMiddleware) string {
	payload := &authmodels.LoginRequestPayload{
		Username: accountsdata.AnonAccountDefaultUsername,
		Scope:    "prn:pantahub.com:apis:/base/all.readonly",
	}

	tokenString, err := CreateUserToken(payload, jwtMiddleware, nil)
	if err != nil {
		return ""
	}

	return tokenString
}

func CreateUserToken(payload *authmodels.LoginRequestPayload, jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) (tokenString string, rerr *utils.RError) {
	var err error
	var scopes []string

	if payload.Scope != "" && payload.Username == accountsdata.AnonAccountDefaultUsername {
		scopes = utils.ScopeStringFilterBy(strings.Fields(payload.Scope), ".readonly", "")
	} else {
		scopes = utils.ScopeStringFilterBy(strings.Fields(payload.Scope), "", "")
	}

	if payload.Username != accountsdata.AnonAccountDefaultUsername && !jwtMiddleware.Authenticator(payload.Username, payload.Password) {
		rerr = &utils.RError{
			Msg:   "Authentication Failed",
			Error: "Authentication Failed",
			Code:  http.StatusUnauthorized,
		}
		return tokenString, rerr
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(jwtMiddleware.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	accExpires := time.Now().Add(jwtMiddleware.Timeout).Unix()
	if jwtMiddleware.PayloadFunc != nil {
		acc := jwtMiddleware.PayloadFunc(payload.Username)
		if acc == nil {
			// no resolvable principal (e.g. an admin call-as of an unknown
			// user): never mint a token that carries no identity claims
			rerr = &utils.RError{
				Msg:   "Authentication Failed",
				Error: "Authentication Failed",
				Code:  http.StatusUnauthorized,
			}
			return tokenString, rerr
		}
		for key, value := range acc {
			if key == "exp" {
				accExpires = value.(int64)
			}
			claims[key] = value
		}
	}

	if payload.Username != accountsdata.AnonAccountDefaultUsername {
		claims["id"] = payload.Username
	}

	claims["exp"] = accExpires

	var authToken *tokenmodels.AuthToken
	// validate if secret is a token
	password, err := base64.RawStdEncoding.DecodeString(payload.Password)
	if err == nil && mongoClient != nil {
		splitPassword := strings.Split(string(password), ":")
		if len(splitPassword) > 1 {
			tokenid := splitPassword[0]
			repo := tokenrepo.New(mongoClient)
			service := tokenservice.New(repo)
			authToken, err = service.GetToken(context.Background(), tokenid, "")
			if err != nil {
				log.Printf("ERROR: service.GetToken: %s", err.Error())
			}
		}
	}

	if authToken != nil && !authToken.Deleted && authToken.ExpireAt.Unix() > time.Now().Unix() &&
		authToken.SecretMatches(payload.Password) {
		scopes = authToken.Scopes
		// identity is the token owner, never the supplied login string
		claims["id"] = authToken.Owner
		claims["nick"] = authToken.Name
		claims["prn"] = authToken.Owner
		claims["roles"] = strings.ToLower(string(authToken.Type))
		claims["type"] = string(authToken.Type)
		// Token can not be refreshed
		claims["orig_iat"] = time.Now().Unix()
		timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 60
		}
		claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	}

	if len(scopes) > 0 {
		claims["scopes"] = strings.Join(scopes, " ")
	}

	if payload.Username == accountsdata.AnonAccountDefaultUsername {
		timeoutStr := utils.GetEnv(utils.EnvAnonJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 5
		}
		claims["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	}

	if jwtMiddleware.MaxRefresh != 0 {
		claims["orig_iat"] = time.Now().Unix()
	}

	tokenString, err = token.SignedString(jwtMiddleware.Key)
	if err != nil {
		rerr = &utils.RError{
			Msg:   "Error signing new token",
			Error: "Error signing new token",
			Code:  http.StatusInternalServerError,
		}
		return tokenString, rerr
	}

	return tokenString, rerr
}

// MintAuthenticatedUserToken builds a session token for a user whose
// identity was already proven (password + second factor, or a passkey
// assertion). It mirrors the claim shape of CreateUserToken but never calls
// the Authenticator: the caller is responsible for having authenticated the
// user. extraClaims (e.g. "amr", "auth_time") are overlaid last but cannot
// override identity or expiry claims.
func MintAuthenticatedUserToken(payload *authmodels.LoginRequestPayload, extraClaims map[string]interface{}, jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) (tokenString string, rerr *utils.RError) {
	scopes := utils.ScopeStringFilterBy(strings.Fields(payload.Scope), "", "")

	token := jwtgo.New(jwtgo.GetSigningMethod(jwtMiddleware.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	protected := map[string]bool{
		"id": true, "prn": true, "nick": true, "roles": true, "type": true,
		"exp": true, "orig_iat": true, "scopes": true,
	}
	for key, value := range extraClaims {
		if !protected[key] {
			claims[key] = value
		}
	}

	accExpires := time.Now().Add(jwtMiddleware.Timeout).Unix()
	if jwtMiddleware.PayloadFunc != nil {
		acc := jwtMiddleware.PayloadFunc(payload.Username)
		if acc == nil {
			rerr = &utils.RError{
				Msg:   "Authentication Failed",
				Error: "Authentication Failed",
				Code:  http.StatusUnauthorized,
			}
			return "", rerr
		}
		for key, value := range acc {
			if key == "exp" {
				accExpires = value.(int64)
			}
			claims[key] = value
		}
	}

	claims["id"] = payload.Username
	claims["exp"] = accExpires

	if len(scopes) > 0 {
		claims["scopes"] = strings.Join(scopes, " ")
	}

	if jwtMiddleware.MaxRefresh != 0 {
		claims["orig_iat"] = time.Now().Unix()
	}

	tokenString, err := token.SignedString(jwtMiddleware.Key)
	if err != nil {
		rerr = &utils.RError{
			Msg:   "Error signing new token",
			Error: "Error signing new token",
			Code:  http.StatusInternalServerError,
		}
		return "", rerr
	}

	return tokenString, nil
}

// CreateBearerFromPersonalToken mints a short-lived JWT iff the supplied
// (username, personalToken) pair resolves to a valid, non-deleted,
// non-expired AuthToken owned by that username. Account passwords are
// NEVER accepted.
//
// Returns 401 RError for: malformed token, tokenid not found, expired,
// deleted, owner mismatch, or secret mismatch.
func CreateBearerFromPersonalToken(
	ctx context.Context,
	username string,
	personalToken string,
	jwtMiddleware *jwt.JWTMiddleware,
	mongoClient *mongo.Client,
	ttl time.Duration,
) (tokenString string, rerr *utils.RError) {
	if jwtMiddleware == nil || mongoClient == nil {
		rerr = &utils.RError{
			Msg:   "Invalid personal token",
			Error: "Invalid personal token",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	// Try RawURLEncoding first (what tokenservice uses), then RawStdEncoding fallback.
	var decoded []byte
	var err error

	decoded, err = base64.RawURLEncoding.DecodeString(personalToken)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(personalToken)
		if err != nil {
			rerr = &utils.RError{
				Msg:   "Invalid personal token format",
				Error: "Invalid personal token format",
				Code:  http.StatusUnauthorized,
			}
			return "", rerr
		}
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		rerr = &utils.RError{
			Msg:   "Invalid personal token format",
			Error: "Invalid personal token format",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	tokenid := parts[0]
	_ = parts[1] // secret is verified below via authToken.SecretMatches

	repo := tokenrepo.New(mongoClient)
	service := tokenservice.New(repo)
	authToken, err := service.GetToken(ctx, tokenid, "")
	if err != nil || authToken == nil {
		rerr = &utils.RError{
			Msg:   "Invalid personal token",
			Error: "Invalid personal token",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	if authToken.Deleted || authToken.ExpireAt.Before(time.Now()) {
		rerr = &utils.RError{
			Msg:   "Invalid personal token",
			Error: "Invalid personal token",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	// Owner check: resolve username to PRN.
	account, err := GetAccount(username, mongoClient)
	if err != nil {
		rerr = &utils.RError{
			Msg:   "Invalid personal token",
			Error: "Invalid personal token",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	if authToken.Owner != account.Prn {
		rerr = &utils.RError{
			Msg:   "Invalid personal token",
			Error: "Invalid personal token",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	// Secret check: the personalToken is the composite base64 string whose
	// SHA-256 must match the stored digest.
	if !authToken.SecretMatches(personalToken) {
		rerr = &utils.RError{
			Msg:   "Invalid personal token",
			Error: "Invalid personal token",
			Code:  http.StatusUnauthorized,
		}
		return "", rerr
	}

	// Build JWT.
	token := jwtgo.New(jwtgo.GetSigningMethod(jwtMiddleware.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	claims["id"] = authToken.Owner
	claims["nick"] = authToken.Name
	claims["prn"] = authToken.Owner
	claims["roles"] = strings.ToLower(string(authToken.Type))
	claims["type"] = string(authToken.Type)
	claims["orig_iat"] = time.Now().Unix()
	claims["exp"] = time.Now().Add(ttl).Unix()

	if len(authToken.Scopes) > 0 {
		claims["scopes"] = strings.Join(authToken.Scopes, " ")
	}

	tokenString, err = token.SignedString(jwtMiddleware.Key)
	if err != nil {
		rerr = &utils.RError{
			Msg:   "Error signing new token",
			Error: "Error signing new token",
			Code:  http.StatusInternalServerError,
		}
		return "", rerr
	}

	return tokenString, nil
}

// IsValidPersonalToken tells whether secret is a valid personal access token
// (PAT) usable to log in as the given account. PATs are the machine channel
// and stay exempt from the two-factor step-up; this check is cheap (no
// password hash comparison) so the login gate can classify the credential
// before deciding whether to demand a second factor.
func IsValidPersonalToken(ctx context.Context, username string, accountPrn string, secret string, mongoClient *mongo.Client) bool {
	if mongoClient == nil || secret == "" {
		return false
	}

	decoded, err := base64.RawStdEncoding.DecodeString(secret)
	if err != nil {
		return false
	}

	splitPassword := strings.Split(string(decoded), ":")
	if len(splitPassword) < 2 {
		return false
	}

	repo := tokenrepo.New(mongoClient)
	service := tokenservice.New(repo)
	authToken, err := service.GetToken(ctx, splitPassword[0], "")
	if err != nil || authToken == nil {
		return false
	}

	if authToken.Deleted || authToken.ExpireAt.Unix() <= time.Now().Unix() {
		return false
	}

	if !authToken.SecretMatches(secret) {
		return false
	}

	// bind strictly to ownership: a PAT authorizes only its owner's account,
	// never an account whose login identifier merely equals the PAT's
	// user-chosen Name
	return authToken.Owner == accountPrn
}

func AuthWithUserPassFactory(mongoClient *mongo.Client) func(string, string) bool {
	return func(userId string, password string) bool {
		var loginUser string

		if userId == "" || password == "" {
			return false
		}

		userTup := strings.SplitN(userId, "==>", 2)
		if len(userTup) > 1 {
			loginUser = userTup[0]
		} else {
			loginUser = userId
		}

		testUserID := loginUser
		if !strings.HasPrefix(loginUser, "prn:") {
			testUserID = "prn:pantahub.com:auth:/" + loginUser
		}

		if strings.HasPrefix(loginUser, utils.BaseServiceID) {
			tpApp, err := apps.LoginAsApp(loginUser, password, mongoClient.Database(utils.MongoDb))
			if err != nil || tpApp == nil || tpApp.Prn == "" {
				return false
			}
			if tpApp.Type != apps.AppTypeConfidential {
				return false
			}
			return true
		}

		plm, ok := accountsdata.DefaultAccounts[testUserID]
		if !ok {
			if strings.HasPrefix(loginUser, "prn:::devices:") {
				return DeviceAuth(loginUser, password, mongoClient)
			}

			return AccountAuth(loginUser, password, mongoClient)
		}

		return plm.Password == password
	}
}

func AuthenticatePayloadFactory(mongoClient *mongo.Client, jwtMiddleware *jwt.JWTMiddleware) func(string) map[string]interface{} {
	return func(userId string) map[string]interface{} {
		var loginUser, callUser string
		var payload map[string]interface{}

		userTup := strings.SplitN(userId, "==>", 2)
		if len(userTup) > 1 {
			loginUser = userTup[0]
			callUser = userTup[1]
		} else {
			loginUser = userId
		}

		testUserID := loginUser
		if !strings.HasPrefix(loginUser, "prn:") {
			testUserID = "prn:pantahub.com:auth:/" + loginUser
		}
		if plm, ok := accountsdata.DefaultAccounts[testUserID]; !ok {
			if loginUser == accountsdata.AnonAccountDefaultUsername {
				payload = AccountToPayload(accounts.CreateAnonAccount())
			} else if strings.HasPrefix(userId, "prn:::devices:") {
				payload = DevicePayload(loginUser, mongoClient)
			} else {
				payload = AccountPayload(loginUser, mongoClient)
			}
		} else {
			payload = AccountToPayload(plm)
		}

		if payload == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			payload, err := apps.GetAppPayload(ctx, userId, mongoClient.Database(utils.MongoDb))
			if err != nil {
				return nil
			}
			return payload
		}

		if callUser != "" && payload["roles"] == "admin" {
			callPayload := jwtMiddleware.PayloadFunc(callUser)
			if callPayload == nil {
				// unknown call-as target: refuse the whole login rather than
				// panic on the nil map
				return nil
			}
			callPayload["id"] = payload["id"].(string) + "==>" + callPayload["id"].(string)
			payload["call-as"] = callPayload
		}

		return payload
	}
}

func GetAccount(prnEmailNick string, mongoClient *mongo.Client) (accounts.Account, error) {

	var (
		err     error
		account accounts.Account
	)
	if strings.HasPrefix(prnEmailNick, "prn:::devices:") {
		return account, errors.New("getAccount does not serve device accounts")
	}

	var ok, ok2 bool
	if account, ok = accountsdata.DefaultAccounts[prnEmailNick]; !ok {
		fullprn := "prn:pantahub.com:auth:/" + prnEmailNick
		account, ok2 = accountsdata.DefaultAccounts[fullprn]
	}

	if ok || ok2 {
		return account, nil
	}

	c := mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	// we accept three variants to identify the account:
	//  - id (pure and with prn format
	//  - email
	//  - nick
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if utils.IsEmail(prnEmailNick) {
		err = c.FindOne(ctx, bson.M{"email": prnEmailNick}).Decode(&account)
	} else if utils.IsNick(prnEmailNick) {
		err = c.FindOne(ctx, bson.M{"nick": prnEmailNick}).Decode(&account)
	} else {
		err = c.FindOne(ctx, bson.M{"prn": prnEmailNick}).Decode(&account)
	}

	return account, err
}

func AccountAuth(idEmailNick string, secret string, mongoClient *mongo.Client) bool {

	var (
		err       error
		account   accounts.Account
		authToken *tokenmodels.AuthToken
	)

	authTokenValid := false

	// validate if secret is a token
	password, err := base64.RawStdEncoding.DecodeString(secret)
	if err == nil && mongoClient != nil {
		splitPassword := strings.Split(string(password), ":")
		if len(splitPassword) > 1 {
			tokenid := splitPassword[0]
			repo := tokenrepo.New(mongoClient)
			service := tokenservice.New(repo)
			authToken, err = service.GetToken(context.Background(), tokenid, account.Prn)
			if err == nil && authToken != nil && !authToken.Deleted && authToken.SecretMatches(secret) && authToken.ExpireAt.Unix() > time.Now().Unix() {
				authTokenValid = true
			}
		}
	}

	if utils.GetEnv(utils.EnvPantahubDisableEmailPasswordLogin) == "true" {
		return false
	}

	account, err = GetAccount(idEmailNick, mongoClient)
	if err != nil {
		return false
	}

	if !IsEmailDomainAllowed(account.Email) {
		return false
	}

	// account has still a challenge -> not activated -> fail to login
	if account.Challenge != "" {
		return false
	}

	// if the token is validated and the username to login is the email or nick it should be true is the token owner is the same
	if authToken != nil && authTokenValid && authToken.Owner == account.Prn {
		return true
	}

	// account has same password as the secret provided to func call -> success
	if utils.CheckPasswordHash(secret, account.PasswordBcrypt, utils.CryptoMethods.BCrypt) {
		return true
	}
	if account.Password != "" && secret == account.Password {
		return true
	}

	// fail by default.
	return false
}

func AccountPayload(idEmailNick string, mongoClient *mongo.Client) map[string]interface{} {
	var (
		err     error
		account accounts.Account
	)

	account, err = GetAccount(idEmailNick, mongoClient)
	account.Password = ""
	account.Challenge = ""

	// error with db or not found -> log and fail
	if err != nil {
		return nil
	}

	return AccountToPayload(account)
}

func DeviceAuth(deviceID string, secret string, mongoClient *mongo.Client) bool {
	id := utils.PrnGetID(deviceID)

	c := mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	mgoID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false
	}

	// read both the hashed secret and, for rows the background migration
	// has not reached yet, the legacy plaintext one
	var stored struct {
		Secret     string `bson:"secret"`
		SecretHash string `bson:"secret_hash"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(mgoID.Hex())
	if err != nil {
		return false
	}
	err = c.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}, options.FindOne().SetProjection(bson.M{"secret": 1, "secret_hash": 1})).Decode(&stored)
	if err != nil {
		return false
	}

	ok, upgrade := utils.VerifyStoredSecret(stored.SecretHash, stored.Secret, secret)
	if ok && upgrade != "" {
		// legacy plaintext matched: upgrade the row in place so it no longer
		// depends on the background migration (plaintext stays until purge)
		_, _ = c.UpdateOne(ctx,
			bson.M{"_id": deviceObjectID, utils.SecretHashField: bson.M{"$exists": false}},
			bson.M{"$set": bson.M{utils.SecretHashField: upgrade}})
	}
	return ok
}

func DevicePayload(deviceID string, mongoClient *mongo.Client) map[string]interface{} {

	c := mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	id := utils.PrnGetID(deviceID)
	mgoID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil
	}

	device := devices.Device{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(mgoID.Hex())
	if err != nil {
		return nil
	}
	err = c.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		return nil
	}

	val := map[string]interface{}{
		"id":     device.Prn,
		"nick":   device.Nick,
		"roles":  "device",
		"type":   "DEVICE",
		"prn":    device.Prn,
		"owner":  device.Owner,
		"scopes": utils.Scopes.API.String(),
	}

	if device.OVMode.NeedsVerification() {
		val["scopes"] = utils.Scopes.APIReadOnly.String() + " " + utils.Scopes.ValidateDevices.String()
		timeoutStr := utils.GetEnv(utils.EnvPendingOVModeJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 5
		}
		val["exp"] = time.Now().Add(time.Minute * time.Duration(timeout)).Unix()
	}

	return val
}

// AccountToPayload get account payload for JWT tokens
func AccountToPayload(account accounts.Account) map[string]interface{} {
	result := map[string]interface{}{}

	switch account.Type {
	case accounts.AccountTypeAdmin:
		result["roles"] = "admin"
		result["type"] = "USER"
	case accounts.AccountTypeUser:
		result["roles"] = "user"
		result["type"] = "USER"
	case accounts.AccountTypeSessionUser:
		result["roles"] = "session"
		result["type"] = "SESSION"
	case accounts.AccountTypeDevice:
		result["roles"] = "device"
		result["type"] = "DEVICE"
	case accounts.AccountTypeService:
		result["roles"] = "service"
		result["type"] = "SERVICE"
	case accounts.AccountTypeClient:
		result["roles"] = "client"
		result["type"] = accounts.AccountTypeClient
	default:
		log.Println("ERROR: AccountToPayload with invalid account type: " + account.Type)
		return nil
	}

	result["id"] = account.Prn
	result["nick"] = account.Nick
	result["prn"] = account.Prn
	result["scopes"] = utils.Scopes.API.String()

	return result
}
