//
// Copyright 2017  Pantacor Ltd.
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
package utils

import "os"

const (
	// Pantahub JWT Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	// default: "THIS MUST BE CHANGED"
	ENV_PANTAHUB_JWT_SECRET = "PANTAHUB_JWT_SECRET"

	// Host you want clients to reach this server under
	// default: localhost
	ENV_PANTAHUB_HOST = "PANTAHUB_HOST"

	// Host you want clients to reach the web-interface
	// default: localhost
	ENV_PANTAHUB_HOST_WWW = "PANTAHUB_HOST_WWW"

	// Port you want to make this server available under
	// default: 12365 for http and 12366 for https
	ENV_PANTAHUB_PORT = "PANTAHUB_PORT"

	// Default scheme to use for urls pointing at this server when we encode
	// them in json or redirect (e.g. for auth)
	// default: http
	ENV_PANTAHUB_SCHEME = "PANTAHUB_SCHEME"

	// XXX: not used
	ENV_PANTAHUB_APIVERSION = "PANTAHUB_APIVERSION"

	// Authentication endpoint to point clients to that need access tokens
	// or need more privileged access tokens.
	// default: $PANTAHUB_SCHEME://$PANTAHUB_HOST:$PANTAHUB_PORT/auth
	ENV_PANTAHUB_AUTH = "PH_AUTH"

	// port to listen to on for http on internal interfaces
	// default: 12365
	ENV_PANTAHUB_PORT_INT = "PANTAHUB_PORT_INT"

	// port to listen to on for https on internal interfaces
	// default: 12366
	ENV_PANTAHUB_PORT_INT_TLS = "PANTAHUB_PORT_INT_TLS"

	// Hostname for mongodb connection
	// default: localhost
	ENV_MONGO_HOST = "MONGO_HOST"

	// Port for mongodb connection
	// default: 27017
	ENV_MONGO_PORT = "MONGO_PORT"

	// Database name for mongodb connection
	// default: pantabase-serv
	ENV_MONGO_DB = "MONGO_DB"

	// Database user for mongodb connection
	// default: <none>
	ENV_MONGO_USER = "MONGO_USER"

	// Database password for mongodb connection
	// default: <none>
	ENV_MONGO_PASS = "MONGO_PASS"

	// Database password for mongodb connection
	// default: <none>
	ENV_MONGO_RS = "MONGO_RS"

	// Service Account Admin Secret to use
	// default: <none> (Required!)
	ENV_PANTAHUB_SA_ADMIN_SECRET = "PANTAHUB_SA_ADMIN_SECRET"

	// SMTP host to use for sending mails
	// default: <none>
	ENV_SMTP_HOST = "SMTP_HOST"

	// SMTP port to use for sending mails
	// default: <none>
	ENV_SMTP_PORT = "SMTP_PORT"

	// SMTP user to use for sending mails
	// default: <none>
	ENV_SMTP_USER = "SMTP_USER"

	// SMTP pass to use for sending mails
	// default: <none>
	ENV_SMTP_PASS = "SMTP_PASS"

	// SMTP pass to use for sending mails
	// default: <none>
	ENV_REG_EMAIL = "REG_EMAIL"

	// PANTAHUB_S3PATH for backing storage
	// default: ../local-s3/
	ENV_PANTAHUB_S3PATH = "PANTAHUB_S3PATH"
)

var defaultEnvs = map[string]string{
	ENV_PANTAHUB_JWT_SECRET: "YOU MUST CHANGE THIS",

	ENV_PANTAHUB_HOST:       "localhost",
	ENV_PANTAHUB_HOST_WWW:   "localhost",
	ENV_PANTAHUB_PORT:       "12365",
	ENV_PANTAHUB_SCHEME:     "http",
	ENV_PANTAHUB_APIVERSION: "", // XXX: needs to move to v0 at least for release

	ENV_PANTAHUB_PORT_INT:     "12365",
	ENV_PANTAHUB_PORT_INT_TLS: "12366",

	ENV_MONGO_HOST: "localhost",
	ENV_MONGO_DB:   "pantabase-serv",
	ENV_MONGO_USER: "",
	ENV_MONGO_PASS: "",
	ENV_MONGO_PORT: "27017",
	ENV_MONGO_RS: "", // replicaSet; needed if connecting to multiple hosts

	ENV_PANTAHUB_SA_ADMIN_SECRET: "",

	ENV_SMTP_HOST: "localhost",
	ENV_SMTP_PORT: "25",
	ENV_SMTP_USER: "XXX",
	ENV_SMTP_PASS: "XXX",
	ENV_REG_EMAIL: "admin@pantacor.com",

	ENV_PANTAHUB_AUTH:   "https://localhost:12366/auth",
	ENV_PANTAHUB_S3PATH: "../local-s3/",
}

func GetEnv(key string) string {
	v, f := os.LookupEnv(key)
	if !f {
		v = defaultEnvs[key]
	}
	return v
}
