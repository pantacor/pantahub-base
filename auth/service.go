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
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	exchangeTokenRequiredErr = "Exchange token is needed"
	passwordIsNeededErr      = "New password is needed"
	//#nosec G101 -- an error message shown to the caller, not a token
	tokenInvalidOrExpiredErr    = "Invalid or expired token"
	emailRequiredForPasswordErr = "Email is required"
	dbConnectionErr             = "Error with Database connectivity"
	emailNotFoundErr            = "Email don't exist"
	tokenCreationErr            = "Error creating token"
	sendEmailErr                = "Error sending email"
	restorePasswordTTLUnit      = time.Minute
)

// App define auth rest application
type App struct {
	jwtConfig    *jwtauth.Config
	mongoClient  *mongo.Client
	mfaRepo      *storage.MFARepo
	webauthnRepo *storage.WebauthnRepo
}

// demoAccountsEnabled tells whether the built-in demo accounts (admin:admin,
// user1:user1, ...) run with their code-default passwords, given the value of
// PANTAHUB_PRODUCTION. Fail closed: only an explicit "false"-like value
// enables them; unset (not configured) is treated as production, so a
// deployment that forgets the variable never ships admin:admin.
func demoAccountsEnabled(production string) bool {
	switch strings.ToLower(strings.TrimSpace(production)) {
	case "false", "0", "no", "off":
		return true
	}
	return false
}

func init() {
	production := os.Getenv("PANTAHUB_PRODUCTION")
	if demoAccountsEnabled(production) {
		//#nosec G706 -- PANTAHUB_PRODUCTION is set by the operator, not a caller
		log.Println("PANTAHUB_PRODUCTION=" + production + ": development mode, built-in demo accounts enabled with default passwords")
		return
	}
	if strings.TrimSpace(production) == "" {
		log.Println("WARNING: PANTAHUB_PRODUCTION is not set; assuming production. Built-in demo accounts are disabled unless PANTAHUB_DEMOACCOUNTS_PASSWORD_<nick> is set. Set PANTAHUB_PRODUCTION=false for a development instance")
	}

	// production: keep only the demo accounts that have an explicit password
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

// safeRefreshHandler refreshes the bearer token with the authorize timeout.
// Tokens without orig_iat (x509, third-party, implicit flows) answer "Token is
// not refreshable". The timeout is passed per call: mutating the shared
// config raced with concurrent logins.
func (app *App) safeRefreshHandler(c *echo.Context) error {
	timeoutStr := utils.GetEnv(utils.EnvPantahubAuthorizeJWTTimeoutMinutes)
	authorizeTimeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		authorizeTimeout = 1920
	}

	token, err := app.jwtConfig.Refresh(c.Request().Header.Get("Authorization"), time.Minute*time.Duration(authorizeTimeout))
	if err != nil {
		c.Response().Header().Set("WWW-Authenticate", app.jwtConfig.WWWAuthenticate())
		if err == jwtauth.ErrNotRefreshable {
			log.Printf("WARN: refresh of a token without orig_iat")
			return echoutil.Error(c, "Token is not refreshable", http.StatusUnauthorized)
		}
		return echoutil.Error(c, "Not Authorized", http.StatusUnauthorized)
	}
	return echoutil.WriteJSON(c, http.StatusOK, map[string]string{"token": token})
}

// New create a new auth rest application
func New(jwtConfig *jwtauth.Config, mongoClient *mongo.Client) *App {
	app := new(App)
	app.jwtConfig = jwtConfig
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
		Keys: bson.D{
			{Key: "nick", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "prn", Value: int32(1)},
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
		Keys: bson.D{
			{Key: "connected_providers.service", Value: int32(1)},
			{Key: "connected_providers.provider_id", Value: int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up connected provider index for pantahub_accounts: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bson.D{
			{Key: "email", Value: int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	// Set Authenticate with user password and generate payload
	jwtConfig.Authenticator = authservices.AuthWithUserPassFactory(mongoClient)
	jwtConfig.PayloadFunc = authservices.AuthenticatePayloadFactory(mongoClient, jwtConfig)

	app.mfaRepo = storage.NewMFARepo(mongoClient)
	if err := app.mfaRepo.SetIndexes(context.Background()); err != nil {
		log.Fatalln("Error setting up indexes for mfa collections: " + err.Error())
		return nil
	}

	app.webauthnRepo = storage.NewWebauthnRepo(mongoClient)
	if err := app.webauthnRepo.SetIndexes(context.Background()); err != nil {
		log.Fatalln("Error setting up indexes for webauthn collections: " + err.Error())
		return nil
	}

	return app
}

// Mount registers auth on echo with its previous middleware stack.
func (app *App) Mount(s *echoutil.Server) {
	const prefix = "/auth"

	g := s.Mount(prefix,
		echoutil.AccessLogJSON(log.New(os.Stdout, "/auth:", log.Lshortfile), prefix),
		echoutil.AccessLogFluent(&utils.AccessLogFluentMiddleware{Prefix: "auth"}, prefix),
		metrics.EchoMiddleware(prefix),
		echoutil.Instrument(),
		echoutil.Recover(),
		echoutil.CORS(echoutil.CORSConfig{
			RejectNonCorsRequests: false,
			OriginValidator:       echoutil.AllowAllOrigins,
			AllowedMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{
				"Accept",
				"Content-Type",
				"Content-Length",
				"X-Custom-Header",
				"Origin",
				"Authorization",
				"X-Trace-ID",
				"Trace-Id",
				"x-request-id",
				"X-Request-ID",
				"TraceID",
				"ParentID",
				"Uber-Trace-ID",
				"uber-trace-id",
				"traceparent",
				"tracestate",
			},
			AccessControlAllowCredentials: true,
			AccessControlMaxAge:           3600,
		}),
		// isWhiteListedForAuthentication returns false for routes without auth
		echoutil.If(prefix, isWhiteListedForAuthentication, echoutil.JWT(app.jwtConfig)),
		echoutil.If(prefix, isWhiteListedForAuthentication, echoutil.Auth()),
	)

	g.GET("/", app.handleGetProfile)
	g.POST("/login", app.getTokenUsingPassword)
	g.POST("/login/mfa/totp", app.handlePostLoginMFATOTP)
	g.POST("/login/mfa/recovery", app.handlePostLoginMFARecovery)
	g.POST("/login/mfa/webauthn", app.handlePostLoginMFAWebauthn)
	g.POST("/login/mfa/webauthn/finish", app.handlePostLoginMFAWebauthnFinish)
	g.POST("/login/webauthn/begin", app.handlePostPasskeyLoginBegin)
	g.POST("/login/webauthn/finish", app.handlePostPasskeyLoginFinish)
	g.GET("/mfa", app.handleGetMFAStatus)
	g.POST("/mfa/totp", app.handlePostTOTPEnroll)
	g.POST("/mfa/totp/confirm", app.handlePostTOTPConfirm)
	g.DELETE("/mfa/totp", app.handleDeleteTOTP)
	g.POST("/mfa/recovery/regenerate", app.handlePostRecoveryRegenerate)
	g.POST("/mfa/reauth/totp", app.handlePostReauthTOTP)
	g.POST("/mfa/reauth/recovery", app.handlePostReauthRecovery)
	g.POST("/mfa/reauth/webauthn", app.handlePostReauthWebauthn)
	g.POST("/mfa/reauth/webauthn/finish", app.handlePostReauthWebauthnFinish)
	g.POST("/mfa/webauthn/register", app.handlePostWebauthnRegister)
	g.POST("/mfa/webauthn/register/finish", app.handlePostWebauthnRegisterFinish)
	g.PATCH("/mfa/webauthn/credentials/:id", app.handlePatchWebauthnCredential)
	g.DELETE("/mfa/webauthn/credentials/:id", app.handleDeleteWebauthnCredential)
	g.GET("/connected-providers", app.handleGetConnectedProviders)
	g.POST("/connected-providers", app.handlePostConnectedProvider)
	g.DELETE("/connected-providers", app.handleDeleteConnectedProvider)
	g.POST("/token", app.handlePostToken)
	g.POST("/token/refresh", app.handlePostTokenRefresh)
	g.GET("/auth_status", handleAuthStatus)
	g.GET("/login", app.safeRefreshHandler)
	g.GET("/accounts", app.handleGetAccounts)
	g.POST("/accounts", app.handlePostAccount)
	g.POST("/sessions", app.handlePostSession)
	g.GET("/verify", app.handleVerify)
	g.POST("/recover", app.handlePasswordRecovery)
	g.POST("/password", app.handlePasswordReset)
	g.POST("/authorize", app.handlePostAuthorizeToken)
	g.POST("/code", app.handlePostCode)
	g.POST("/signature/verify", app.verifyToken)
	g.POST("/x509/login", app.handleAuthUsingDeviceCert)
	g.GET("/oauth/login/:service", app.HandleGetThirdPartyLogin)
	g.GET("/oauth/callback/:service", app.HandleGetThirdPartyCallback)
	g.POST("/oauth/token", app.HandlePKCEToken)
	g.GET("/oauth/authorize", app.HandlePKCEAuthorize)
	g.POST("/oauth/authorize", app.HandlePostPKCEAuthorize)
	g.POST("/oauth/pkce/init", app.HandlePostPKCEInit)
}

func handleGetEncryptedAccount(accountData *authmodels.AccountCreationPayload) (*authmodels.EncryptedAccountToken, error) {
	encryptedAccountData, err := utils.CreateJWE(accountData)
	if err != nil {
		return nil, err
	}

	urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://"
	urlPrefix += utils.GetEnv(utils.EnvPantahubWWWHost)
	urlPrefix += utils.GetEnv(utils.EnvPantahubSignupPath)
	urlPrefix += "#account=" + encryptedAccountData

	response := &authmodels.EncryptedAccountToken{
		Token:       encryptedAccountData,
		RedirectURI: urlPrefix,
	}

	return response, nil
}

func (a *App) getAccountPayload(idEmailNick string) map[string]interface{} {
	var plm accounts.Account
	var ok, ok2 bool

	plm, ok = accountsdata.DefaultAccounts[idEmailNick]
	if ok {
		return authservices.AccountToPayload(plm)
	}

	fullprn := "prn:pantahub.com:auth:/" + idEmailNick
	plm, ok2 = accountsdata.DefaultAccounts[fullprn]
	if ok2 {
		return authservices.AccountToPayload(plm)
	}

	if strings.HasPrefix(idEmailNick, "prn:::devices:") {
		return authservices.DevicePayload(idEmailNick, a.mongoClient)
	}

	acc := authservices.AccountPayload(idEmailNick, a.mongoClient)
	if acc != nil && acc["prn"] != nil {
		return acc
	}

	return authservices.AccountToPayload(plm)
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
func isWhiteListedForAuthentication(request *http.Request) bool {
	// This function determines if authentication middleware should be applied.
	// It returns `true` if authentication is REQUIRED for the request.
	// It returns `false` if authentication is NOT REQUIRED (i.e., the path is whitelisted for skipping authentication).

	// List of conditions where authentication is NOT required (i.e., the path is whitelisted).
	// If any of these conditions are met, we return false, indicating no authentication is needed.

	// Exact path and method matches. Method-gated so a future route added
	// under /login cannot silently inherit the auth-skip: only password
	// login (POST) and the refresh handler (GET) are exempt today.
	if request.URL.Path == "/login" && (request.Method == "POST" || request.Method == "GET") {
		return false
	}
	// second step of a two-factor login: authenticated by the single-use
	// MFA-pending token carried in the body, not by a session JWT
	if strings.HasPrefix(request.URL.Path, "/login/mfa/") && request.Method == "POST" {
		return false
	}
	// usernameless passkey sign-in: authenticated by the WebAuthn assertion
	if strings.HasPrefix(request.URL.Path, "/login/webauthn/") && request.Method == "POST" {
		return false
	}
	if request.URL.Path == "/accounts" && request.Method == "POST" {
		return false
	}
	if request.URL.Path == "/sessions" && request.Method == "POST" {
		return false
	}
	if request.URL.Path == "/verify" && request.Method == "GET" {
		return false
	}
	if request.URL.Path == "/recover" && request.Method == "POST" {
		return false
	}
	if request.URL.Path == "/password" && request.Method == "POST" {
		return false
	}
	if request.URL.Path == "/signature/verify" && request.Method == "POST" {
		return false
	}
	if request.URL.Path == "/x509/login" && request.Method == "POST" {
		return false
	}

	// Path prefix and method matches for OAuth endpoints
	if strings.HasPrefix(request.URL.Path, "/oauth/token") && request.Method == "POST" {
		return false
	}
	if strings.HasPrefix(request.URL.Path, "/oauth/pkce/init") && request.Method == "POST" {
		return false
	}
	if strings.HasPrefix(request.URL.Path, "/oauth/authorize") && request.Method == "GET" {
		return false
	}
	if strings.HasPrefix(request.URL.Path, "/oauth/login/") && request.Method == "GET" {
		return false
	}
	if strings.HasPrefix(request.URL.Path, "/oauth/callback/") && request.Method == "GET" {
		return false
	}

	// If none of the above conditions are met, then the request is NOT whitelisted
	// for skipping authentication. Therefore, authentication IS required.
	return true
}
