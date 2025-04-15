package authservices

import (
	"context"
	"encoding/base64"
	"errors"
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

	if jwtMiddleware.PayloadFunc != nil {
		acc := jwtMiddleware.PayloadFunc(payload.Username)
		for key, value := range acc {
			claims[key] = value
		}
	}

	if payload.Username != accountsdata.AnonAccountDefaultUsername {
		claims["id"] = payload.Username
	}

	claims["exp"] = time.Now().Add(jwtMiddleware.Timeout).Unix()

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

	if authToken != nil && !authToken.Deleted && authToken.ExpireAt.Unix() > time.Now().Unix() {
		scopes = authToken.Scopes
		claims["id"] = payload.Username
		claims["nick"] = authToken.Name
		claims["prn"] = authToken.Owner
		claims["roles"] = strings.ToLower(string(authToken.Type))
		claims["type"] = string(authToken.Type)
		// Token can not be refreshed
		claims["orig_iat"] = time.Now().Unix()
		timeoutStr := utils.GetEnv(utils.EnvAnonJWTTimeoutMinutes)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			timeout = 5
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
			if err != nil || tpApp == nil {
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
			if err == nil && authToken != nil && !authToken.Deleted && authToken.Secret == secret && authToken.ExpireAt.Unix() > time.Now().Unix() {
				authTokenValid = true
			}
		}
	}

	// if token is valid and the username for login is the token name login true
	if authToken != nil && authTokenValid && authToken.Name == idEmailNick {
		return true
	}

	account, err = GetAccount(idEmailNick, mongoClient)
	if err != nil {
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

	device := devices.Device{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(mgoID.Hex())
	if err != nil {
		return false
	}
	err = c.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		return false
	}
	if secret == device.Secret {
		return true
	}
	return false
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
		"scopes": "prn:pantahub.com:apis:/base/all",
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
	result["scopes"] = "prn:pantahub.com:apis:/base/all"

	return result
}
