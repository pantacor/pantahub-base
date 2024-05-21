//
// Copyright 2019  Pantacor Ltd.
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

package base

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"github.com/rs/cors"
	httpSwagger "github.com/swaggo/http-swagger"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/auth"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/callbacks"
	"gitlab.com/pantacor/pantahub-base/changes"
	"gitlab.com/pantacor/pantahub-base/cron"
	"gitlab.com/pantacor/pantahub-base/dash"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/exports"
	"gitlab.com/pantacor/pantahub-base/healthz"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/plog"
	"gitlab.com/pantacor/pantahub-base/profiles"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/tokens"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"

	_ "gitlab.com/pantacor/pantahub-base/docs" // docs is generated by Swag CLI, you have to import it.
)

// DoInit init pantahub REST Aplication
func DoInit() {
	if err := LoadDynamicS3ByRegion(); err != nil {
		panic(err)
	}

	phAuth := utils.GetEnv(utils.EnvPantahubAuth)
	jwtSecretBase64 := utils.GetEnv(utils.EnvPantahubJWTAuthSecret)
	jwtSecretPem, err := base64.StdEncoding.DecodeString(jwtSecretBase64)
	if err != nil {
		panic(fmt.Errorf("no valid JWT secret (PANTAHUB_JWT_AUTH_SECRET) in base64 format: %s", err.Error()))
	}
	jwtSecret, err := jwtgo.ParseRSAPrivateKeyFromPEM(jwtSecretPem)
	if err != nil {
		panic(fmt.Errorf("no valid JWT secret (PANTAHUB_JWT_AUTH_SECRET); must be rsa private key in PEM format: %s", err.Error()))
	}

	jwtPubBase64 := utils.GetEnv(utils.EnvPantahubJWTAuthPub)
	jwtPubPem, err := base64.StdEncoding.DecodeString(jwtPubBase64)
	if err != nil {
		panic(fmt.Errorf("no valid JWT PUB KEY (PANTAHUB_JWT_AUTH_PUB) in base64 format: %s", err.Error()))
	}
	jwtPub, err := jwtgo.ParseRSAPublicKeyFromPEM(jwtPubPem)
	if err != nil {
		panic(fmt.Errorf("no valid JWT pub key (PANTAHUB_JWT_AUTH_PUB); must be rsa private key in PEM format: %s", err.Error()))
	}

	mongoClient, _ := utils.GetMongoClient()

	timeoutStr := utils.GetEnv(utils.EnvPantahubJWTTimeoutMinutes)
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		panic(err)
	}

	maxRefreshStr := utils.GetEnv(utils.EnvPantahubJWTMaxRefreshMinutes)
	maxRefresh, err := strconv.Atoi(maxRefreshStr)
	if err != nil {
		panic(err)
	}

	defaultJwtMiddleware := &jwt.JWTMiddleware{
		Key:              jwtSecret,
		Pub:              jwtPub,
		Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
		Authenticator:    falseAuthenticator,
		Timeout:          time.Minute * time.Duration(timeout),
		MaxRefresh:       time.Minute * time.Duration(maxRefresh),
		SigningAlgorithm: "RS256",
	}

	defaultJwtMiddleware.Authenticator = authservices.AuthWithUserPassFactory(mongoClient)
	defaultJwtMiddleware.PayloadFunc = authservices.AuthenticatePayloadFactory(mongoClient, defaultJwtMiddleware)

	adminUsers := utils.GetSubscriptionAdmins()
	subService := subscriptions.NewService(mongoClient, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)

	{
		app := auth.New(&jwt.JWTMiddleware{
			Key:              jwtSecret,
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Timeout:          time.Minute * time.Duration(timeout),
			MaxRefresh:       time.Minute * time.Duration(maxRefresh),
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/auth/", http.StripPrefix("/auth", app.API.MakeHandler()))
	}
	{
		app := objects.New(defaultJwtMiddleware, subService, mongoClient)
		http.Handle("/objects/", http.StripPrefix("/objects", app.API.MakeHandler()))
	}
	{
		app := changes.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/changes/", http.StripPrefix("/changes", app.API.MakeHandler()))
	}
	{
		app := devices.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/devices/", http.StripPrefix("/devices", app.API.MakeHandler()))
	}
	{
		app := trails.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/trails/", http.StripPrefix("/trails", app.API.MakeHandler()))
	}
	{
		app := plog.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/plog/", http.StripPrefix("/plog", app.API.MakeHandler()))
	}
	{
		app := logs.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/logs/", http.StripPrefix("/logs", app.API.MakeHandler()))
	}
	{
		app := healthz.New(mongoClient)
		http.Handle("/healthz/", http.StripPrefix("/healthz", app.API.MakeHandler()))
	}
	{
		app := dash.New(defaultJwtMiddleware, subService, mongoClient)
		http.Handle("/dash/", http.StripPrefix("/dash", app.API.MakeHandler()))
	}
	{
		app := subscriptions.New(defaultJwtMiddleware, subService, mongoClient)
		http.Handle("/subscriptions/", http.StripPrefix("/subscriptions", app.MakeHandler()))
	}
	{
		app := metrics.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/metrics/", http.StripPrefix("/metrics", app.API.MakeHandler()))
	}
	{
		app := apps.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/apps/", http.StripPrefix("/apps", app.API.MakeHandler()))
	}
	{
		app := profiles.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/profiles/", http.StripPrefix("/profiles", app.API.MakeHandler()))
	}
	{
		cronJobTimeout, err := strconv.Atoi(utils.GetEnv(utils.EnvCronJobTimeout))
		if err != nil {
			panic(fmt.Errorf("error Parsing CRON_JOB_TIMEOUT: %s", err.Error()))
		}

		app := cron.New(defaultJwtMiddleware, (time.Duration(cronJobTimeout) * time.Second),
			mongoClient)
		http.Handle("/cron/", http.StripPrefix("/cron", app.API.MakeHandler()))
	}
	{
		app := callbacks.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/callbacks/", http.StripPrefix("/callbacks", app.API.MakeHandler()))
	}
	{
		app := exports.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/exports/", http.StripPrefix("/exports", app.API.MakeHandler()))
	}
	{
		app := tokens.New(defaultJwtMiddleware, mongoClient)
		http.Handle("/tokens/", http.StripPrefix("/tokens", app.API.MakeHandler()))
	}

	var fservermux FileUploadServer
	switch utils.GetEnv(utils.EnvPantahubStorageDriver) {
	case "s3":
		log.Println("INFO: using 's3' driver to serve object blobs/files")
		fservermux = NewS3FileServer()
	default:
		log.Println("INFO: using 'local' driver to serve object blobs/files")
		fservermux = &LocalFileServer{fileServer: http.FileServer(http.Dir(utils.PantahubS3Path())), directory: utils.PantahubS3Path()}
	}

	// default cors - allow GET and POST from all origins
	fserver := cors.AllowAll().Handler(fservermux)

	// @deprecated
	http.Handle("/local-s3/", http.StripPrefix("/local-s3", fserver))

	// handle s3 storage request's
	http.Handle("/s3/", http.StripPrefix("/s3", fserver))

	http.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"), //The url pointing to API definition"
	))

}
