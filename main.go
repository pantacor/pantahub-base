// Copyright 2017  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"

	"gitlab.com/pantacor/pantahub-base/base"
	"gitlab.com/pantacor/pantahub-base/docs"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
)

// @title Pantahub API reference
// @version 1.0
// @description This is the pantahub API documentation to use our API.

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @securityDefinitions.basic BasicAuth
// @tokenUrl /auth/login

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization

// @securitydefinitions.oauth2.application OAuth2Application
// @tokenUrl /auth/token
// @scope.write Grants write access
// @scope.admin Grants read and write access to administrative information

// @securitydefinitions.oauth2.implicit OAuth2Implicit
// @authorizationUrl /auth/authorize
// @scope.write Grants write access
// @scope.admin Grants read and write access to administrative information

// @securitydefinitions.oauth2.password OAuth2Password
// @tokenUrl /auth/token
// @scope.read Grants read access
// @scope.write Grants write access
// @scope.admin Grants read and write access to administrative information

// @securitydefinitions.oauth2.accessCode OAuth2AccessCode
// @tokenUrl /auth/token
// @authorizationUrl /auth/authorize

// @BasePath /
func main() {

	utils.InitScopes()
	base.DoInit()

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" && os.Getenv("OTEL_SERVICE_NAME") != "" {
		tp := tracer.Init(os.Getenv("OTEL_SERVICE_NAME"))
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Printf("Error shutting down tracer provider: %v", err)
			}
		}()
	}

	docs.SwaggerInfo.BasePath = "/"
	docs.SwaggerInfo.Schemes = []string{utils.GetEnv(utils.EnvPantahubScheme)}
	docs.SwaggerInfo.Host = utils.GetEnv(utils.EnvPantahubHost)

	portInt := utils.GetEnv(utils.EnvPantahubPortInt)
	portIntTLS := utils.GetEnv(utils.EnvPantahubPortIntTLS)

	go func() {
		log.Fatal(http.ListenAndServeTLS(":"+portIntTLS, "localhost.cert.pem", "localhost.key.pem", nil))
	}()

	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			log.Printf("Serving @ https://" + ip.String() + ":" + portIntTLS + "/\n")
			log.Printf("Serving @ http://" + ip.String() + ":" + portInt + "/\n")
		}
	}
	log.Fatal(http.ListenAndServe(":"+portInt, nil))
}
