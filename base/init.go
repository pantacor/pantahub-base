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
	"log"
	"net/http"
	"strconv"
	"time"

	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"github.com/rs/cors"
	"gitlab.com/pantacor/pantahub-base/auth"
	"gitlab.com/pantacor/pantahub-base/dash"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/healthz"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/plog"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func DoInit() {

	phAuth := utils.GetEnv(utils.ENV_PANTAHUB_AUTH)
	jwtSecret := utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET)

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
			Key:        []byte(jwtSecret),
			Realm:      "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Timeout:    time.Minute * time.Duration(timeout),
			MaxRefresh: time.Hour * time.Duration(maxRefresh),
		}, mongoClient)
		http.Handle("/auth/", http.StripPrefix("/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, mongoClient)
		http.Handle("/objects/", http.StripPrefix("/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, mongoClient)
		http.Handle("/devices/", http.StripPrefix("/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, mongoClient)
		http.Handle("/trails/", http.StripPrefix("/trails", app.Api.MakeHandler()))
	}
	{
		app := plog.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, mongoClient)
		http.Handle("/plog/", http.StripPrefix("/plog", app.Api.MakeHandler()))
	}
	{
		app := logs.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, mongoClient)
		http.Handle("/logs/", http.StripPrefix("/logs", app.Api.MakeHandler()))
	}

	{
		app := healthz.New(mongoClient)
		http.Handle("/healthz/", http.StripPrefix("/healthz", app.Api.MakeHandler()))
	}
	{
		app := dash.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, mongoClient)
		http.Handle("/dash/", http.StripPrefix("/dash", app.Api.MakeHandler()))
	}
	{
		app := subscriptions.NewResty(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, mongoClient)
		http.Handle("/subscriptions/", http.StripPrefix("/subscriptions", app.MakeHandler()))
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
