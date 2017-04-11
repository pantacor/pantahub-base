//
// Copyright 2017  Alexander Sack <asac129@gmail.com>
//
package utils

import "os"

const (
	ENV_PANTAHUB_HOST       = "PANTAHUB_HOST"
	ENV_PANTAHUB_PORT       = "PANTAHUB_PORT"
	ENV_PANTAHUB_SCHEME     = "PANTAHUB_SCHEME"
	ENV_PANTAHUB_APIVERSION = "PANTAHUB_APIVERSION"
	ENV_PANTAHUB_AUTH       = "PH_AUTH"

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
