//
// Copyright 2017  Alexander Sack <asac129@gmail.com>
//
package utils

import "os"

var defaultEnvs = map[string]string{
	"PANTAHUB_HOST":   "localhost",
	"PANTAHUB_PORT":   "12365",
	"PANTAHUB_SCHEME": "http",

	"MONGO_HOST": "localhost",
	"MONGO_DB":   "pantabase-serv",

	"SMTP_HOST": "localhost",
	"SMTP_PORT": "25",
	"SMTP_USER": "XXX",
	"SMTP_PASS": "XXX",

	"PH_AUTH": "https://localhost:12366/auth",
}

func GetEnv(key string) string {
	v, f := os.LookupEnv(key)
	if !f {
		v = defaultEnvs[key]
	}
	return v
}
