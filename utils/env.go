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
	ENV_PANTAHUB_HOST       = "PANTAHUB_HOST"
	ENV_PANTAHUB_PORT       = "PANTAHUB_PORT"
	ENV_PANTAHUB_SCHEME     = "PANTAHUB_SCHEME"
	ENV_PANTAHUB_APIVERSION = "PANTAHUB_APIVERSION"
	ENV_PANTAHUB_AUTH       = "PH_AUTH"

	ENV_PANTAHUB_PORT_INT     = "PANTAHUB_PORT_INT"
	ENV_PANTAHUB_PORT_INT_TLS = "PANTAHUB_PORT_INT_TLS"

	ENV_MONGO_HOST = "MONGO_HOST"
	ENV_MONGO_PORT = "MONGO_PORT"
	ENV_MONGO_DB   = "MONGO_DB"
	ENV_MONGO_USER = "MONGO_USER"
	ENV_MONGO_PASS = "MONGO_PASS"
	ENV_SMTP_HOST  = "SMTP_HOST"
	ENV_SMTP_PORT  = "SMTP_PORT"
	ENV_SMTP_USER  = "SMTP_USER"
	ENV_SMTP_PASS  = "SMTP_PASS"
)

var defaultEnvs = map[string]string{
	ENV_PANTAHUB_HOST:       "localhost",
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

	ENV_SMTP_HOST: "localhost",
	ENV_SMTP_PORT: "25",
	ENV_SMTP_USER: "XXX",
	ENV_SMTP_PASS: "XXX",

	ENV_PANTAHUB_AUTH: "https://localhost:12366/auth",
}

func GetEnv(key string) string {
	v, f := os.LookupEnv(key)
	if !f {
		v = defaultEnvs[key]
	}
	return v
}
