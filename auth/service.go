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
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
)

// AccountType Defines the type of account
type AccountType string

const (
	exchangeTokenRequiredErr    = "Exchange token is needed"
	passwordIsNeededErr         = "New password is needed"
	tokenInvalidOrExpiredErr    = "Invalid or expired token"
	emailRequiredForPasswordErr = "Email is required"
	dbConnectionErr             = "Error with Database connectivity"
	emailNotFoundErr            = "Email don't exist"
	tokenCreationErr            = "Error creating token"
	sendEmailErr                = "Error sending email"
	restorePasswordTTLUnit      = time.Minute
)

type tokenResponse struct {
	Token       string `json:"token"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	TokenType   string `json:"token_type"`
	Scopes      string `json:"scopes"`
}

// App define auth rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

type encryptedAccountToken struct {
	Token       string `json:"token"`
	RedirectURI string `json:"redirect-uri"`
}

type accountCreationPayload struct {
	accounts.Account
	Captcha          string `json:"captcha"`
	EncryptedAccount string `json:"encrypted-account"`
}

type passwordReset struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type resetPasswordClaims struct {
	Email        string    `json:"email"`
	TimeModified time.Time `json:"time-modified"`
	jwtgo.StandardClaims
}

// this requests to swap access code with accesstoken
type tokenRequest struct {
	Code    string `json:"access-code"`
	Comment string `json:"comment"`
}

type tokenStore struct {
	ID      primitive.ObjectID     `json:"id" bson:"_id"`
	Client  string                 `json:"client"`
	Owner   string                 `json:"owner"`
	Comment string                 `json:"comment"`
	Claims  map[string]interface{} `json:"jwt-claims"`
}

func init() {
	// if in production we disable all fixed accounts
	if os.Getenv("PANTAHUB_PRODUCTION") == "" {
		return
	}

	for k, v := range accountsdata.DefaultAccounts {
		passwordOverwrite := os.Getenv("PANTAHUB_DEMOACCOUNTS_PASSWORD_" + v.Nick)
		if passwordOverwrite == "" {
			delete(accountsdata.DefaultAccounts, k)
		} else {
			log.Println("enabling default account: " + v.Nick)
			v.Password = passwordOverwrite
			accountsdata.DefaultAccounts[k] = v
		}
	}
}

// New create a new auth rest application
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtMiddleware = jwtMiddleware
	app.mongoClient = mongoClient

	//key := flag.String("nick", "", "The field you'd like to place an index on")
	//unique := flag.Bool("unique", true, "Would you like the index to be unique?")
	//value := flag.Int("type", 1, "would you like the index to be ascending (1) or descending (-1)?")
	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "email", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}
	jwtMiddleware.Authenticator = func(userId string, password string) bool {

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
			tpApp, err := apps.LoginAsApp(loginUser, password, app.mongoClient.Database(utils.MongoDb))
			if err != nil || tpApp == nil {
				return false
			}
			return true
		}

		plm, ok := accountsdata.DefaultAccounts[testUserID]
		if !ok {
			if strings.HasPrefix(loginUser, "prn:::devices:") {
				return app.deviceAuth(loginUser, password)
			}

			return app.accountAuth(loginUser, password)
		}

		return plm.Password == password
	}

	jwtMiddleware.PayloadFunc = func(userId string) map[string]interface{} {

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
			if strings.HasPrefix(userId, "prn:::devices:") {
				payload = app.devicePayload(loginUser)
			} else {
				payload = app.accountPayload(loginUser)
			}
		} else {
			payload = AccountToPayload(plm)
		}

		if payload == nil {
			payload, err = apps.GetAppPayload(userId, app.mongoClient.Database(utils.MongoDb))
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

	app.API = rest.NewApi()
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/auth:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "auth"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&rest.TimerMiddleware{})
	app.API.Use(&metrics.Middleware{})
	app.API.Use(rest.DefaultDevStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	// no authentication needed for
	app.API.Use(&rest.IfMiddleware{
		Condition: isWhiteListedForAuthentication,
		IfTrue:    app.jwtMiddleware,
	})

	// no authentication needed for
	app.API.Use(&rest.IfMiddleware{
		Condition: isWhiteListedForAuthentication,
		IfTrue:    &utils.AuthMiddleware{},
	})

	// /login /auth_status and /refresh_token endpoints
	apiRouter, _ := rest.MakeRouter(
		rest.Get("/", app.handleGetProfile),
		rest.Post("/login", func(writer rest.ResponseWriter, request *rest.Request) {
			userAgent := request.Header.Get("User-Agent")
			if userAgent == "" {
				utils.RestErrorWrapperUser(writer, "No Access (DOS) - no UserAgent", "Incompatible Client; upgrade pantavisor", http.StatusForbidden)
				return
			}
			app.jwtMiddleware.LoginHandler(writer, request)
		}),
		rest.Post("/token", app.handlePostToken),
		rest.Get("/auth_status", handleAuthStatus),
		rest.Get("/login", app.jwtMiddleware.RefreshHandler),
		rest.Get("/accounts", app.handleGetAccounts),
		rest.Post("/accounts", app.handlePostAccount),
		rest.Post("/sessions", app.handlePostSession),
		rest.Get("/verify", app.handleVerify),
		rest.Post("/recover", app.handlePasswordRecovery),
		rest.Post("/password", app.handlePasswordReset),
		rest.Post("/authorize", app.handlePostAuthorizeToken),
		rest.Post("/code", app.handlePostCode),
		rest.Post("/signature/verify", app.verifyToken),
		rest.Post("/x509/login", app.handleAuthUsingDeviceCert),
		rest.Get("/oauth/login/#service", app.HandleGetThirdPartyLogin),
		rest.Get("/oauth/callback/#service", app.HandleGetThirdPartyCallback),
	)
	app.API.SetApp(apiRouter)

	return app
}

// AccountToPayload get account payload for JWT tokens
func AccountToPayload(account accounts.Account) map[string]interface{} {
	result := map[string]interface{}{}

	switch account.Type {
	case accounts.AccountTypeAdmin:
		result["roles"] = "admin"
		result["type"] = "USER"
		break
	case accounts.AccountTypeUser:
		result["roles"] = "user"
		result["type"] = "USER"
		break
	case accounts.AccountTypeSessionUser:
		result["roles"] = "session"
		result["type"] = "SESSION"
		break
	case accounts.AccountTypeDevice:
		result["roles"] = "device"
		result["type"] = "DEVICE"
		break
	case accounts.AccountTypeService:
		result["roles"] = "service"
		result["type"] = "SERVICE"
		break
	case accounts.AccountTypeClient:
		result["roles"] = "service"
		result["type"] = "SERVICE"
		break
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

func handleGetEncryptedAccount(accountData *accountCreationPayload) (*encryptedAccountToken, error) {
	encryptedAccountData, err := utils.CreateJWE(accountData)
	if err != nil {
		return nil, err
	}

	urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://"
	urlPrefix += utils.GetEnv(utils.EnvPantahubWWWHost)
	urlPrefix += utils.GetEnv(utils.EnvPantahubSignupPath)
	urlPrefix += "#account=" + encryptedAccountData

	response := &encryptedAccountToken{
		Token:       encryptedAccountData,
		RedirectURI: urlPrefix,
	}

	return response, nil
}

func (a *App) getAccount(prnEmailNick string) (accounts.Account, error) {

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

	c := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

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

func (a *App) accountAuth(idEmailNick string, secret string) bool {

	var (
		err     error
		account accounts.Account
	)

	account, err = a.getAccount(idEmailNick)

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

func (a *App) getAccountPayload(idEmailNick string) map[string]interface{} {
	var plm accounts.Account
	var ok, ok2 bool

	plm, ok = accountsdata.DefaultAccounts[idEmailNick]
	if ok {
		return AccountToPayload(plm)
	}

	fullprn := "prn:pantahub.com:auth:/" + idEmailNick
	plm, ok2 = accountsdata.DefaultAccounts[fullprn]
	if ok2 {
		return AccountToPayload(plm)
	}

	if strings.HasPrefix(idEmailNick, "prn:::devices:") {
		return a.devicePayload(idEmailNick)
	}

	acc := a.accountPayload(idEmailNick)
	if acc != nil && acc["prn"] != nil {
		return acc
	}

	return AccountToPayload(plm)
}

func (a *App) accessCodePayload(userIDEmailNick string, serviceIDEmailNick string, scopes string) map[string]interface{} {
	var (
		userAccountPayload    map[string]interface{}
		serviceAccountPayload map[string]interface{}
	)

	serviceAccountPayload = a.getAccountPayload(serviceIDEmailNick)
	userAccountPayload = a.getAccountPayload(userIDEmailNick)

	// error with db or not found -> log and fail
	if serviceAccountPayload == nil {
		return nil
	}

	if userAccountPayload == nil {
		return nil
	}

	accessCodePayload := map[string]interface{}{}
	accessCodePayload["approver_prn"] = userAccountPayload["prn"]
	accessCodePayload["approver_nick"] = userAccountPayload["nick"]
	accessCodePayload["approver_roles"] = userAccountPayload["roles"]
	accessCodePayload["approver_type"] = userAccountPayload["type"]
	accessCodePayload["service"] = serviceAccountPayload["prn"]
	accessCodePayload["scopes"] = scopes

	return accessCodePayload
}

func (a *App) accountPayload(idEmailNick string) map[string]interface{} {
	var (
		err     error
		account accounts.Account
	)

	account, err = a.getAccount(idEmailNick)
	account.Password = ""
	account.Challenge = ""

	// error with db or not found -> log and fail
	if err != nil {
		return nil
	}

	return AccountToPayload(account)
}

func (a *App) deviceAuth(deviceID string, secret string) bool {
	id := utils.PrnGetID(deviceID)

	c := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

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

func (a *App) devicePayload(deviceID string) map[string]interface{} {

	c := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

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

func isWhiteListedForAuthentication(request *rest.Request) bool {
	return request.URL.Path != "/login" &&
		!(request.URL.Path == "/accounts" && request.Method == "POST") &&
		!(request.URL.Path == "/sessions" && request.Method == "POST") &&
		!(request.URL.Path == "/verify" && request.Method == "GET") &&
		!(request.URL.Path == "/recover" && request.Method == "POST") &&
		!(request.URL.Path == "/password" && request.Method == "POST") &&
		!(request.URL.Path == "/signature/verify" && request.Method == "POST") &&
		!(request.URL.Path == "/x509/login" && request.Method == "POST") &&
		!(strings.HasPrefix(request.URL.Path, "/oauth/login/") && request.Method == "GET") &&
		!(strings.HasPrefix(request.URL.Path, "/oauth/callback/") && request.Method == "GET")
}
