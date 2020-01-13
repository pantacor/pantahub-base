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
	"gitlab.com/pantacor/pantahub-base/auth"
	"gitlab.com/pantacor/pantahub-base/dash"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/healthz"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/plog"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/apps"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func DoInit() {

	phAuth := utils.GetEnv(utils.ENV_PANTAHUB_AUTH)
	jwtSecretBase64 := utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET)
	jwtSecretPem, err := base64.StdEncoding.DecodeString(jwtSecretBase64)
	if err != nil {
		panic(fmt.Errorf("No valid JWT secret (PANTAHUB_JWT_AUTH_SECRET) in base64 format: %s", err.Error()))
	}
	jwtSecret, err := jwtgo.ParseRSAPrivateKeyFromPEM(jwtSecretPem)
	if err != nil {
		panic(fmt.Errorf("No valid JWT secret (PANTAHUB_JWT_AUTH_SECRET); must be rsa private key in PEM format: %s", err.Error()))
	}

	jwtPubBase64 := utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_PUB)
	jwtPubPem, err := base64.StdEncoding.DecodeString(jwtPubBase64)
	if err != nil {
		panic(fmt.Errorf("No valid JWT PUB KEY (PANTAHUB_JWT_AUTH_PUB) in base64 format: %s", err.Error()))
	}
	jwtPub, err := jwtgo.ParseRSAPublicKeyFromPEM(jwtPubPem)
	if err != nil {
		panic(fmt.Errorf("No valid JWT pub key (PANTAHUB_JWT_AUTH_PUB); must be rsa private key in PEM format: %s", err.Error()))
	}

	mongoClient, _ := utils.GetMongoClient()

	adminUsers := utils.GetSubscriptionAdmins()
	subService := subscriptions.NewService(mongoClient, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)

	{
		timeoutStr := utils.GetEnv(utils.ENV_PANTAHUB_JWT_TIMEOUT_MINUTES)
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			panic(err)
		}

		maxRefreshStr := utils.GetEnv(utils.ENV_PANTAHUB_JWT_MAX_REFRESH_MINUTES)
		maxRefresh, err := strconv.Atoi(maxRefreshStr)
		if err != nil {
			panic(err)
		}

		app := auth.New(&jwt.JWTMiddleware{
			Key:              jwtSecret,
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Timeout:          time.Minute * time.Duration(timeout),
			MaxRefresh:       time.Minute * time.Duration(maxRefresh),
			SigningAlgorithm: "RS256",
			LogFunc: func(text string) {
				log.Println("/auth: " + text)
			},
		}, mongoClient)
		http.Handle("/auth/", http.StripPrefix("/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, subService, mongoClient)
		http.Handle("/objects/", http.StripPrefix("/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/devices/", http.StripPrefix("/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/trails/", http.StripPrefix("/trails", app.Api.MakeHandler()))
	}
	{
		app := plog.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/plog/", http.StripPrefix("/plog", app.Api.MakeHandler()))
	}
	{
		app := logs.New(&jwt.JWTMiddleware{
			Key:              jwtSecret,
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/logs/", http.StripPrefix("/logs", app.Api.MakeHandler()))
	}

	{
		app := healthz.New(mongoClient)
		http.Handle("/healthz/", http.StripPrefix("/healthz", app.Api.MakeHandler()))
	}
	{
		app := dash.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, subService, mongoClient)
		http.Handle("/dash/", http.StripPrefix("/dash", app.Api.MakeHandler()))
	}
	{
		app := subscriptions.NewResty(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, subService, mongoClient)
		http.Handle("/subscriptions/", http.StripPrefix("/subscriptions", app.MakeHandler()))
	}
	{
		app := metrics.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/metrics/", http.StripPrefix("/metrics", app.Api.MakeHandler()))
	}
	{
		app := apps.New(&jwt.JWTMiddleware{
			Pub:              jwtPub,
			Realm:            "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator:    falseAuthenticator,
			SigningAlgorithm: "RS256",
		}, mongoClient)
		http.Handle("/apps/", http.StripPrefix("/apps", app.API.MakeHandler()))
	}

	var fservermux FileUploadServer
	switch utils.GetEnv(utils.ENV_PANTAHUB_STORAGE_DRIVER) {
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
}
