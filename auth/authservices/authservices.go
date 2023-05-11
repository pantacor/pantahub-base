package authservices

import (
	"context"
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
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// AccountType Defines the type of account
type AccountType string

type TokenResponse struct {
	Token       string `json:"token"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	State       string `json:"state,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
	Scopes      string `json:"scopes,omitempty"`
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
}

type TokenStore struct {
	ID      primitive.ObjectID     `json:"id" bson:"_id"`
	Client  string                 `json:"client"`
	Owner   string                 `json:"owner"`
	Comment string                 `json:"comment"`
	Claims  map[string]interface{} `json:"jwt-claims"`
}

type LoginRequestPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Scope    string `json:"scope"`
}

func CreateAnonToken(jwtMiddleware *jwt.JWTMiddleware) string {
	payload := &LoginRequestPayload{
		Username: accountsdata.AnonAccountDefaultUsername,
		Scope:    "prn:pantahub.com:apis:/base/all.readonly",
	}

	tokenString, err := CreateUserToken(payload, jwtMiddleware)
	if err != nil {
		return ""
	}

	return tokenString
}

func CreateUserToken(payload *LoginRequestPayload, jwtMiddleware *jwt.JWTMiddleware) (tokenString string, rerr *utils.RError) {
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
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(jwtMiddleware.SigningAlgorithm))
	claims := token.Claims.(jwtgo.MapClaims)

	if jwtMiddleware.PayloadFunc != nil {
		for key, value := range jwtMiddleware.PayloadFunc(payload.Username) {
			claims[key] = value
		}
	}

	if payload.Username != accountsdata.AnonAccountDefaultUsername {
		claims["id"] = payload.Username
	}

	claims["exp"] = time.Now().Add(jwtMiddleware.Timeout).Unix()

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
		return
	}

	return
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		err     error
		account accounts.Account
	)

	account, err = GetAccount(idEmailNick, mongoClient)

	// error with db or not found -> log and fail
	if err != nil {
		return false
	}

	// account has still a challenge -> not activated -> fail to login
	if account.Challenge != "" {
		return false
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		result["roles"] = "service"
		result["type"] = "SERVICE"
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
