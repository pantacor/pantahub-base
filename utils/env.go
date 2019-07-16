// Copyright 2017,2018  Pantacor Ltd.
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

import (
	"os"
)

const (
	// Pantahub Product Name (branding)
	ENV_PANTAHUB_PRODUCTNAME = "PANTAHUB_PRODUCTNAME"

	// Pantahub JWT Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	// default: "THIS MUST BE CHANGED"
	ENV_PANTAHUB_JWT_AUTH_SECRET = "PANTAHUB_JWT_SECRET"
	ENV_PANTAHUB_SCRYPT_SECRET   = "PANTAHUB_SCRYPT_SECRET"

	// Pantahub JWT Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	// default: "THIS MUST BE CHANGED"
	ENV_PANTAHUB_JWT_OBJECT_SECRET = "PANTAHUB_JWT_OBJECT_SECRET"

	// Pantahub JWT Token Timeout in Minutes
	// default: 60
	ENV_PANTAHUB_JWT_TIMEOUT_MINUTES = "PANTAHUB_JWT_TIMEOUT_MINUTES"

	// Pantahub JWT Token for password recovery Timeout in Minutes
	// default: 60
	ENV_PANTAHUB_RECOVER_JWT_TIMEOUT_MINUTES = "PANTAHUB_RECOVER_JWT_TIMEOUT_MINUTES"

	// Pantahub JWT Max Refresh timeout in Minutes
	// default: 24 * 60
	ENV_PANTAHUB_JWT_MAX_REFRESH_MINUTES = "PANTAHUB_JWT_MAX_REFRESH_MINUTES"

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

	// Set elasticsearch base URL
	// default: https://es5.pantahub.com
	ENV_ELASTIC_URL = "ELASTIC_URL"

	// Set elasticsearch basic auth username; if set
	// a Basic auth token will be generated for you from
	// ELASTIC_USERNAME & ELASTIC_PASSWORD
	// default: ""
	ENV_ELASTIC_USERNAME = "ELASTIC_USERNAME"

	// Set elasticsearch basic auth password
	// default: ""
	ENV_ELASTIC_PASSWORD = "ELASTIC_PASSWORD"

	// Set elasticsearch bearer auth token
	// default: ""
	ENV_ELASTIC_BEARER = "ELASTIC_BEARER"

	// Set Fluent port to send logs to
	// default: "24224"
	ENV_FLUENT_PORT = "FLUENT_PORT"

	// Set Fluent host to send logs to
	// default: "127.0.0.1"
	ENV_FLUENT_HOST = "FLUENT_HOST"

	// Set K8S NAMESPACE info
	// default: "NA"
	ENV_K8S_NAMESPACE = "K8S_NAMESPACE"

	// Set HOSTNAME info
	// default: "localhost"
	ENV_HOSTNAME = "HOSTNAME"

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

	// mailgone domain
	// default: <empty>
	ENV_MAILGUN_DOMAIN = "MAILGUN_DOMAIN"

	// mailgone api key
	// default: <empty>
	ENV_MAILGUN_APIKEY = "MAILGUN_APIKEY"

	// mailgone pub api key
	// default: <empty>
	ENV_MAILGUN_PUBAPIKEY = "MAILGUN_PUBAPIKEY"

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

	// Comma Separated List of PRNs of users that have pantahub admin role
	// default: <none> (Required for Production!)
	ENV_PANTAHUB_ADMIN_SECRET = "PANTAHUB_ADMIN_SECRET"

	// Comma Separated List of PRNs of users that have pantahub admin role
	// default: <none>
	ENV_PANTAHUB_ADMINS = "PANTAHUB_ADMINS"

	// Comma Separated List of PRNs of users that have pantahub subscription admin
	// role
	// default: <none>
	ENV_PANTAHUB_SUBSCRIPTION_ADMINS = "PANTAHUB_SUBSCRIPTION_ADMINS"

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

	// PANTAHUB_STORAGE_DRIVER used to store objects
	ENV_PANTAHUB_STORAGE_DRIVER = "PANTAHUB_STORAGE_DRIVER"

	// PANTAHUB_S3_ACCESS_KEY_ID access key of s3 storage credentials
	ENV_PANTAHUB_S3_ACCESS_KEY_ID = "PANTAHUB_S3_ACCESS_KEY_ID"

	// PANTAHUB_S3_SECRET_ACCESS_KEY secret access key of s3 storage credentials
	ENV_PANTAHUB_S3_SECRET_ACCESS_KEY = "PANTAHUB_S3_SECRET_ACCESS_KEY"

	// PANTAHUB_S3_USE_ANONYMOUS_CREDENTIALS use anonymous credentials
	ENV_PANTAHUB_S3_USE_ANONYMOUS_CREDENTIALS = "PANTAHUB_S3_USE_ANONYMOUS_CREDENTIALS"

	// PANTAHUB_S3_REGION region where to store objects
	ENV_PANTAHUB_S3_REGION = "PANTAHUB_S3_REGION"

	// PANTAHUB_S3_BUCKET bucket where to store objects
	ENV_PANTAHUB_S3_BUCKET = "PANTAHUB_S3_BUCKET"

	// PANTAHUB_S3_ENDPOINT enpoint of s3 server
	ENV_PANTAHUB_S3_ENDPOINT = "PANTAHUB_S3_ENDPOINT"

	// PANTAHUB_STORAGE_PATH for backing storage
	// default: ../local-s3/
	ENV_PANTAHUB_STORAGE_PATH = "PANTAHUB_STORAGE_PATH"

	// PANTAHUB_S3PATH deprecated, please use ENV_PANTAHUB_STORAGE_PATH instead
	ENV_PANTAHUB_S3PATH = "PANTAHUB_S3PATH"

	// enable resty client debugging if env is set
	// default: ""
	ENV_RESTY_DEBUG = "RESTY_DEBUG"

	// Pantahub GC API end point
	ENV_PANTAHUB_GC_API = "PANTAHUB_GC_API"

	// Pantahub GC garbage removal flag
	ENV_PANTAHUB_GC_REMOVE_GARBAGE = "PANTAHUB_GC_REMOVE_GARBAGE"

	// Pantahub GC UnClaimed expiry for device to mark it as garbage
	ENV_PANTAHUB_GC_UNCLAIMED_EXPIRY = "PANTAHUB_GC_UNCLAIMED_EXPIRY"

	// Pantahub GC garbage expiry time to remove it
	ENV_PANTAHUB_GC_GARBAGE_EXPIRY = "PANTAHUB_GC_GARBAGE_EXPIRY"

	// Pantahub Demo Account:service1 password
	ENV_PANTAHUB_DEMOACCOUNTS_PASSWORD_service1 = "PANTAHUB_DEMOACCOUNTS_PASSWORD_service1"
)

var defaultEnvs = map[string]string{
	ENV_PANTAHUB_PRODUCTNAME:                    "pantahub-personal",
	ENV_PANTAHUB_DEMOACCOUNTS_PASSWORD_service1: "O9i8HlpSc",
	ENV_PANTAHUB_JWT_AUTH_SECRET:                "YOU MUST CHANGE THIS",
	ENV_PANTAHUB_SCRYPT_SECRET:                  "YOU MUST CHANGE THIS",
	ENV_PANTAHUB_JWT_OBJECT_SECRET:              "YOU MUST CHANGE THIS",
	ENV_PANTAHUB_JWT_TIMEOUT_MINUTES:            "60",
	ENV_PANTAHUB_RECOVER_JWT_TIMEOUT_MINUTES:    "60",
	ENV_PANTAHUB_JWT_MAX_REFRESH_MINUTES:        "1440",

	ENV_PANTAHUB_HOST:       "localhost",
	ENV_PANTAHUB_HOST_WWW:   "localhost",
	ENV_PANTAHUB_PORT:       "12365",
	ENV_PANTAHUB_SCHEME:     "http",
	ENV_PANTAHUB_APIVERSION: "", // XXX: needs to move to v0 at least for release

	ENV_PANTAHUB_PORT_INT:     "12365",
	ENV_PANTAHUB_PORT_INT_TLS: "12366",

	// K8S info
	ENV_K8S_NAMESPACE: "NA",

	// HOSTNAME
	ENV_HOSTNAME: "localhost",

	// mailgun support for mail
	ENV_MAILGUN_APIKEY:    "",
	ENV_MAILGUN_DOMAIN:    "",
	ENV_MAILGUN_PUBAPIKEY: "",

	ENV_MONGO_HOST: "localhost",
	ENV_MONGO_DB:   "pantabase-serv",
	ENV_MONGO_USER: "",
	ENV_MONGO_PASS: "",
	ENV_MONGO_PORT: "27017",
	ENV_MONGO_RS:   "", // replicaSet; needed if connecting to multiple hosts

	// elastic search access
	ENV_ELASTIC_URL:      "http://localhost:9200",
	ENV_ELASTIC_USERNAME: "",
	ENV_ELASTIC_PASSWORD: "",
	ENV_ELASTIC_BEARER:   "",

	// fluent vars
	ENV_FLUENT_PORT: "24224",

	// disable by default; to enable set this env...
	ENV_FLUENT_HOST: "",

	// secrets - required!!
	ENV_PANTAHUB_SA_ADMIN_SECRET: "",
	ENV_PANTAHUB_ADMIN_SECRET:    "",

	// roles/admins
	ENV_PANTAHUB_ADMINS:              "prn:pantahub.com:auth:/admin",
	ENV_PANTAHUB_SUBSCRIPTION_ADMINS: "",

	// smtp config (we stopped supporting this in favour of mailgun for launch)
	ENV_SMTP_HOST: "localhost",
	ENV_SMTP_PORT: "25",
	ENV_SMTP_USER: "XXX",
	ENV_SMTP_PASS: "XXX",
	ENV_REG_EMAIL: "Pantahub.com <team@pantacor.com>",

	// pantahub internal envs
	ENV_PANTAHUB_AUTH: "http://localhost:12365/auth",

	// storage driver used to store objects
	ENV_PANTAHUB_STORAGE_DRIVER: "local",

	// access key of s3 storage credentials
	ENV_PANTAHUB_S3_ACCESS_KEY_ID: "",

	// secret access key of s3 storage credentials
	ENV_PANTAHUB_S3_SECRET_ACCESS_KEY: "",

	// use anonymous credentials
	ENV_PANTAHUB_S3_USE_ANONYMOUS_CREDENTIALS: "true",

	// region where to store objects
	ENV_PANTAHUB_S3_REGION: "us-east-1",

	// bucket where to store objects
	ENV_PANTAHUB_S3_BUCKET: "pantahub",

	// enpoint of s3 server
	ENV_PANTAHUB_S3_ENDPOINT: "",

	// object storage path (when using "local" driver)
	ENV_PANTAHUB_STORAGE_PATH: "/",

	// object storage path (deprecated, please use PANTAHUB_STORAGE_PATH)
	ENV_PANTAHUB_S3PATH: "../local-s3/",

	// resty REST client configs
	ENV_RESTY_DEBUG: "",

	// Pantahub GC API end point
	ENV_PANTAHUB_GC_API: "http://localhost:2000",

	// Pantahub GC garbage removal flag
	ENV_PANTAHUB_GC_REMOVE_GARBAGE: "false",

	/* Pantahub GC UnClaimed expiry for device to mark it as garbage:
	   If a device is unclaimed for 5 Days then it will be marked as garbage

	   Format:ISO_8601: https://en.wikipedia.org/wiki/ISO_8601?oldformat=true#Durations
	*/
	ENV_PANTAHUB_GC_UNCLAIMED_EXPIRY: "P5D", // => 5 Days

	/* Pantahub GC garbage expiry time to remove it:
	Once a device/trail/step/object is marked as
	garbage it will be removed after 2 days

	Format:ISO_8601: https://en.wikipedia.org/wiki/ISO_8601?oldformat=true#Durations
	*/
	ENV_PANTAHUB_GC_GARBAGE_EXPIRY: "P2D", // => 2 Days
}

func GetEnv(key string) string {
	v, f := os.LookupEnv(key)
	if !f {
		v = defaultEnvs[key]
	}

	return v
}
