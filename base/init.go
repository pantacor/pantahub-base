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
	"net/http"
	"time"

	jwt "github.com/fundapps/go-json-rest-middleware-jwt"
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

	session, _ := utils.GetMongoSession()

	adminUsers := utils.GetSubscriptionAdmins()
	subService := subscriptions.NewService(session, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)

	{
		app := auth.New(&jwt.JWTMiddleware{
			Key:        []byte(jwtSecret),
			Realm:      "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Timeout:    time.Minute * 60,
			MaxRefresh: time.Hour * 24,
		}, session)
		http.Handle("/auth/", http.StripPrefix("/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, session)
		http.Handle("/objects/", http.StripPrefix("/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/devices/", http.StripPrefix("/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/trails/", http.StripPrefix("/trails", app.Api.MakeHandler()))
	}
	{
		app := plog.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/plog/", http.StripPrefix("/plog", app.Api.MakeHandler()))
	}
	{
		app := logs.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/logs/", http.StripPrefix("/logs", app.Api.MakeHandler()))
	}

	{
		app := healthz.New(session)
		http.Handle("/healthz/", http.StripPrefix("/healthz", app.Api.MakeHandler()))
	}
	{
		app := dash.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, session)
		http.Handle("/dash/", http.StripPrefix("/dash", app.Api.MakeHandler()))
	}
	{
		app := subscriptions.NewResty(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, session)
		http.Handle("/subscriptions/", http.StripPrefix("/subscriptions", app.MakeHandler()))
	}

	var fservermux FileUploadServer
	switch utils.GetEnv(utils.ENV_PANTAHUB_STORAGE_DRIVER) {
	case "s3":
		fservermux = NewS3FileUploadServer()
	default:
		fservermux = &LocalFileUploadServer{fileServer: http.FileServer(http.Dir(objects.PantahubS3Path())), directory: objects.PantahubS3Path()}
	}

	// default cors - allow GET and POST from all origins
	fserver := cors.AllowAll().Handler(fservermux)

	// @deprecated
	http.Handle("/local-s3/", http.StripPrefix("/local-s3", fserver))

	// handle s3 storage request's
	http.Handle("/s3/", http.StripPrefix("/s3", fserver))
}
