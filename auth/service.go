// Copyright 2026 Pantacor Ltd.
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

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/auth/storage"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

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

// App define auth rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
	mfaRepo       *storage.MFARepo
	webauthnRepo  *storage.WebauthnRepo
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

// safeRefreshHandler wraps jwtMiddleware.RefreshHandler to recover from panics
// caused by tokens missing the "orig_iat" claim (e.g. from x509, third-party,
// or implicit auth flows). The upstream RefreshHandler has an unchecked type
// assertion on orig_iat that panics when the claim is nil.
func (app *App) safeRefreshHandler(w rest.ResponseWriter, r *rest.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("WARN: RefreshHandler recovered from panic: %v", rec)
			w.Header().Set("WWW-Authenticate", "JWT realm="+app.jwtMiddleware.Realm)
			rest.Error(w, "Token is not refreshable", http.StatusUnauthorized)
		}
	}()

	originalTimeout := app.jwtMiddleware.Timeout
	timeoutStr := utils.GetEnv(utils.EnvPantahubAuthorizeJWTTimeoutMinutes)
	authorizeTimeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		authorizeTimeout = 1920
	}
	app.jwtMiddleware.Timeout = time.Minute * time.Duration(authorizeTimeout)
	defer func() {
		app.jwtMiddleware.Timeout = originalTimeout
	}()

	app.jwtMiddleware.RefreshHandler(w, r)
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
	jwtMiddleware.Authenticator = authservices.AuthWithUserPassFactory(mongoClient)
	jwtMiddleware.PayloadFunc = authservices.AuthenticatePayloadFactory(mongoClient, jwtMiddleware)

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

	app.API = rest.NewApi()
	app.API.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/auth:", log.Lshortfile)})
	app.API.Use(&utils.AccessLogFluentMiddleware{Prefix: "auth"})
	app.API.Use(&rest.StatusMiddleware{})
	app.API.Use(&metrics.Middleware{})
	app.API.Use(rest.DefaultCommonStack...)
	app.API.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
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
		rest.Post("/login", app.getTokenUsingPassword),
		rest.Post("/login/mfa/totp", app.handlePostLoginMFATOTP),
		rest.Post("/login/mfa/recovery", app.handlePostLoginMFARecovery),
		rest.Post("/login/mfa/webauthn", app.handlePostLoginMFAWebauthn),
		rest.Post("/login/mfa/webauthn/finish", app.handlePostLoginMFAWebauthnFinish),
		rest.Post("/login/webauthn/begin", app.handlePostPasskeyLoginBegin),
		rest.Post("/login/webauthn/finish", app.handlePostPasskeyLoginFinish),
		rest.Get("/mfa", app.handleGetMFAStatus),
		rest.Post("/mfa/totp", app.handlePostTOTPEnroll),
		rest.Post("/mfa/totp/confirm", app.handlePostTOTPConfirm),
		rest.Delete("/mfa/totp", app.handleDeleteTOTP),
		rest.Post("/mfa/recovery/regenerate", app.handlePostRecoveryRegenerate),
		rest.Post("/mfa/reauth/totp", app.handlePostReauthTOTP),
		rest.Post("/mfa/reauth/recovery", app.handlePostReauthRecovery),
		rest.Post("/mfa/reauth/webauthn", app.handlePostReauthWebauthn),
		rest.Post("/mfa/reauth/webauthn/finish", app.handlePostReauthWebauthnFinish),
		rest.Post("/mfa/webauthn/register", app.handlePostWebauthnRegister),
		rest.Post("/mfa/webauthn/register/finish", app.handlePostWebauthnRegisterFinish),
		rest.Patch("/mfa/webauthn/credentials/#id", app.handlePatchWebauthnCredential),
		rest.Delete("/mfa/webauthn/credentials/#id", app.handleDeleteWebauthnCredential),
		rest.Get("/connected-providers", app.handleGetConnectedProviders),
		rest.Post("/connected-providers", app.handlePostConnectedProvider),
		rest.Delete("/connected-providers", app.handleDeleteConnectedProvider),
		rest.Post("/token", app.handlePostToken),
		rest.Post("/token/refresh", app.handlePostTokenRefresh),
		rest.Get("/auth_status", handleAuthStatus),
		rest.Get("/login", app.safeRefreshHandler),
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
		rest.Post("/oauth/token", app.HandlePKCEToken),
		rest.Get("/oauth/authorize", app.HandlePKCEAuthorize),
		rest.Post("/oauth/authorize", app.HandlePostPKCEAuthorize),
		rest.Post("/oauth/pkce/init", app.HandlePostPKCEInit),
	)
	app.API.Use(&tracer.OtelMiddleware{
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
		Router:      apiRouter,
	})
	app.API.SetApp(apiRouter)

	return app
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
func isWhiteListedForAuthentication(request *rest.Request) bool {
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
