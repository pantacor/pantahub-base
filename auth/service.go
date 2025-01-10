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
	"log"
	"os"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
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

	// Set Authenticate with user password and generate payload
	jwtMiddleware.Authenticator = authservices.AuthWithUserPassFactory(mongoClient)
	jwtMiddleware.PayloadFunc = authservices.AuthenticatePayloadFactory(mongoClient, jwtMiddleware)

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
