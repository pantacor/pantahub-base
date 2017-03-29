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
}

func GetEnv(key string) string {
	v, f := os.LookupEnv(key)
	if !f {
		v = defaultEnvs[key]
	}
	return v
}
