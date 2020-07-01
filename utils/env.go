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
	// EnvPantahubProductName Pantahub Product Name (branding)
	EnvPantahubProductName = "PANTAHUB_PRODUCTNAME"

	// EnvPantahubJWTAuthSecret Pantahub JWT Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	// default: "THIS MUST BE CHANGED"
	EnvPantahubJWTAuthSecret = "PANTAHUB_JWT_SECRET"

	// EnvPantahubScryptSecret scrypt secret
	EnvPantahubScryptSecret = "PANTAHUB_SCRYPT_SECRET"

	// EnvPantahubJWTAuthPub Pantahub JWT Public Key. Public RSA key in base64 encoded PEM format
	EnvPantahubJWTAuthPub = "PANTAHUB_JWT_PUB"

	// EnvPantahubJWESecret Pantahub JWE Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	EnvPantahubJWESecret = "PANTAHUB_JWE_SECRET"

	// EnvPantahubJWEPub Pantahub JWE Public Key. Public RSA key in base64 encoded PEM format
	EnvPantahubJWEPub = "PANTAHUB_JWE_PUB"

	// EnvGoogleCaptchaSecret Google Captcha service secret key
	// default: "This must be changed"
	EnvGoogleCaptchaSecret = "GOOGLE_CAPTCHA_SECRET"

	// EnvPantahubUseCaptcha Pantahub Use Captcha. Set if captcha will be used by the API.
	EnvPantahubUseCaptcha = "PANTAHUB_USE_CAPTCHA"

	// EnvPantahubJWTObjectSecret Pantahub JWT Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	// default: "THIS MUST BE CHANGED"
	EnvPantahubJWTObjectSecret = "PANTAHUB_JWT_OBJECT_SECRET"

	// EnvPantahubJWTTimeoutMinutes Pantahub JWT Token Timeout in Minutes
	// default: 60
	EnvPantahubJWTTimeoutMinutes = "PANTAHUB_JWT_TIMEOUT_MINUTES"

	// EnvPantahubRecoverJWTTimeoutMinutes Pantahub JWT Token for password recovery Timeout in Minutes
	// default: 60
	EnvPantahubRecoverJWTTimeoutMinutes = "PANTAHUB_RECOVER_JWT_TIMEOUT_MINUTES"

	// EnvPantahubJWTMaxRefreshMinutes Pantahub JWT Max Refresh timeout in Minutes
	// default: 24 * 60
	EnvPantahubJWTMaxRefreshMinutes = "PANTAHUB_JWT_MAX_REFRESH_MINUTES"

	// EnvPantahubHost Host you want clients to reach this server under
	// default: localhost
	EnvPantahubHost = "PANTAHUB_HOST"

	// EnvPantahubWWWHost Host you want clients to reach the web-interface
	// default: localhost
	EnvPantahubWWWHost = "PANTAHUB_HOST_WWW"

	// EnvPantahubPort Port you want to make this server available under
	// default: 12365 for http and 12366 for https
	EnvPantahubPort = "PANTAHUB_PORT"

	// EnvPantahubScheme Default scheme to use for urls pointing at this server when we encode
	// them in json or redirect (e.g. for auth)
	// default: http
	EnvPantahubScheme = "PANTAHUB_SCHEME"

	// EnvPantahubAPIVersion not used
	EnvPantahubAPIVersion = "PANTAHUB_APIVERSION"

	// EnvElasticURL Set elasticsearch base URL
	// default: https://es5.pantahub.com
	EnvElasticURL = "ELASTIC_URL"

	// EnvElasticUsername Set elasticsearch basic auth username; if set
	// a Basic auth token will be generated for you from
	// ELASTIC_USERNAME & ELASTIC_PASSWORD
	// default: ""
	EnvElasticUsername = "ELASTIC_USERNAME"

	// EnvElasticPassword Set elasticsearch basic auth password
	// default: ""
	EnvElasticPassword = "ELASTIC_PASSWORD"

	// EnvElasticBearer Set elasticsearch bearer auth token
	// default: ""
	EnvElasticBearer = "ELASTIC_BEARER"

	// EnvFluentPort Set Fluent port to send logs to
	// default: "24224"
	EnvFluentPort = "FLUENT_PORT"

	// EnvFluentHost Set Fluent host to send logs to
	// default: "127.0.0.1"
	EnvFluentHost = "FLUENT_HOST"

	// EnvK8SNamespace Set K8S NAMESPACE info
	// default: "NA"
	EnvK8SNamespace = "K8S_NAMESPACE"

	// EnvHostName Set HOSTNAME info
	// default: "localhost"
	EnvHostName = "HOSTNAME"

	// EnvPantahubAuth Authentication endpoint to point clients to that need access tokens
	// or need more privileged access tokens.
	// default: $PANTAHUB_SCHEME://$PANTAHUB_HOST:$PANTAHUB_PORT/auth
	EnvPantahubAuth = "PH_AUTH"

	// EnvPantahubSignupPath pantahub signup path
	EnvPantahubSignupPath = "PANTAHUB_SIGNUP_PATH"

	// EnvPantahubPortInt port to listen to on for http on internal interfaces
	// default: 12365
	EnvPantahubPortInt = "PANTAHUB_PORT_INT"

	// EnvPantahubPortIntTLS port to listen to on for https on internal interfaces
	// default: 12366
	EnvPantahubPortIntTLS = "PANTAHUB_PORT_INT_TLS"

	// EnvMailgunDomain domain
	// default: <empty>
	EnvMailgunDomain = "MAILGUN_DOMAIN"

	// EnvMailgunAPIKey api key
	// default: <empty>
	EnvMailgunAPIKey = "MAILGUN_APIKEY"

	// EnvMailgunPubAPIKey mailgone pub api key
	// default: <empty>
	EnvMailgunPubAPIKey = "MAILGUN_PUBAPIKEY"

	// EnvMongoHost Hostname for mongodb connection
	// default: localhost
	EnvMongoHost = "MONGO_HOST"

	// EnvMongoPort Port for mongodb connection
	// default: 27017
	EnvMongoPort = "MONGO_PORT"

	// EnvMongoDb Database name for mongodb connection
	// default: pantabase-serv
	EnvMongoDb = "MONGO_DB"

	// EnvMongoUser Database user for mongodb connection
	// default: <none>
	EnvMongoUser = "MONGO_USER"

	// EnvMongoPassword Database password for mongodb connection
	// default: <none>
	EnvMongoPassword = "MONGO_PASS"

	// EnvMongoRs Database password for mongodb connection
	// default: <none>
	EnvMongoRs = "MONGO_RS"

	// EnvPantahubSaAdminSecret Service Account Admin Secret to use
	// default: <none> (Required!)
	EnvPantahubSaAdminSecret = "PANTAHUB_SA_ADMIN_SECRET"

	// EnvPantahubAdminSecret Comma Separated List of PRNs of users that have pantahub admin role
	// default: <none> (Required for Production!)
	EnvPantahubAdminSecret = "PANTAHUB_ADMIN_SECRET"

	// EnvPantahubAdmins Comma Separated List of PRNs of users that have pantahub admin role
	// default: <none>
	EnvPantahubAdmins = "PANTAHUB_ADMINS"

	// EnvPantahubSubscriptionAdmins Comma Separated List of PRNs of users that have pantahub subscription admin
	// role
	// default: <none>
	EnvPantahubSubscriptionAdmins = "PANTAHUB_SUBSCRIPTION_ADMINS"

	// EnvSMTPHost SMTP host to use for sending mails
	// default: <none>
	EnvSMTPHost = "SMTP_HOST"

	// EnvSMTPPort SMTP port to use for sending mails
	// default: <none>
	EnvSMTPPort = "SMTP_PORT"

	// EnvSMTPUser SMTP user to use for sending mails
	// default: <none>
	EnvSMTPUser = "SMTP_USER"

	// EnvSMTPPass SMTP pass to use for sending mails
	// default: <none>
	EnvSMTPPass = "SMTP_PASS"

	// EnvRegEmail SMTP pass to use for sending mails
	// default: <none>
	EnvRegEmail = "REG_EMAIL"

	// EnvPantahubStorageDriver used to store objects
	EnvPantahubStorageDriver = "PANTAHUB_STORAGE_DRIVER"

	// EnvPantahubS3AccessKeyID access key of s3 storage credentials
	EnvPantahubS3AccessKeyID = "PANTAHUB_S3_ACCESS_KEY_ID"

	// EnvPantahubS3SecretAccessKeyID secret access key of s3 storage credentials
	EnvPantahubS3SecretAccessKeyID = "PANTAHUB_S3_SECRET_ACCESS_KEY"

	// EnvPantahubS3SAnonymousCredentials use anonymous credentials
	EnvPantahubS3SAnonymousCredentials = "PANTAHUB_S3_USE_ANONYMOUS_CREDENTIALS"

	// EnvPantahubS3Region region where to store objects
	EnvPantahubS3Region = "PANTAHUB_S3_REGION"

	// EnvPantahubS3Bucket bucket where to store objects
	EnvPantahubS3Bucket = "PANTAHUB_S3_BUCKET"

	// EnvPantahubS3Endpoint enpoint of s3 server
	EnvPantahubS3Endpoint = "PANTAHUB_S3_ENDPOINT"

	// EnvPantahubStoragePath for backing storage
	// default: ../local-s3/
	EnvPantahubStoragePath = "PANTAHUB_STORAGE_PATH"

	// EnvPantahubS3Path deprecated, please use EnvPantahubStoragePath instead
	EnvPantahubS3Path = "PANTAHUB_S3PATH"

	// EnvRestyDebug enable resty client debugging if env is set
	// default: ""
	EnvRestyDebug = "RESTY_DEBUG"

	// EnvPantahubGCAPI Pantahub GC API end point
	EnvPantahubGCAPI = "PANTAHUB_GC_API"

	// EnvPantahubGCRemoveGarbage Pantahub GC garbage removal flag
	EnvPantahubGCRemoveGarbage = "PANTAHUB_GC_REMOVE_GARBAGE"

	// EnvPantahubGCUnclaimedExpiry Pantahub GC UnClaimed expiry for device to mark it as garbage
	EnvPantahubGCUnclaimedExpiry = "PANTAHUB_GC_UNCLAIMED_EXPIRY"

	// EnvPantahubGCGarbageExpiry Pantahub GC garbage expiry time to remove it
	EnvPantahubGCGarbageExpiry = "PANTAHUB_GC_GARBAGE_EXPIRY"

	// EnvPantahubDemoAccountsPasswordService1 Pantahub Demo Account:service1 password
	EnvPantahubDemoAccountsPasswordService1 = "PANTAHUB_DEMOACCOUNTS_PASSWORD_service1"

	// EnvPantahubLogBody enable log requests,responses parameters and bodies
	EnvPantahubLogBody = "PANTAHUB_LOG_BODY"

	// EnvCronJobTimeout is to set the cron job timeout(secs)
	EnvCronJobTimeout = "CRON_JOB_TIMEOUT"

	// EnvGoogleOAuthClientID GOOGLE_OAUTH_CLIENT_ID
	EnvGoogleOAuthClientID = "GOOGLE_OAUTH_CLIENT_ID"

	// EnvGoogleOAuthClientSecret GOOGLE_OAUTH_CLIENT_SECRET
	EnvGoogleOAuthClientSecret = "GOOGLE_OAUTH_CLIENT_SECRET"

	// EnvGithubOAuthClientID GITHUB_OAUTH_CLIENT_ID
	EnvGithubOAuthClientID = "GITHUB_OAUTH_CLIENT_ID"

	// EnvGithubOAuthClientSecret GITHUB_OAUTH_CLIENT_SECRET
	EnvGithubOAuthClientSecret = "GITHUB_OAUTH_CLIENT_SECRET"

	// EnvGitlabOAuthClientID GITLAB_OAUTH_CLIENT_ID
	EnvGitlabOAuthClientID = "GITLAB_OAUTH_CLIENT_ID"

	// EnvGitlabOAuthClientSecret GITLAB_OAUTH_CLIENT_SECRET
	EnvGitlabOAuthClientSecret = "GITLAB_OAUTH_CLIENT_SECRET"
)

var defaultEnvs = map[string]string{
	EnvPantahubProductName:                  "pantahub-personal",
	EnvPantahubDemoAccountsPasswordService1: "O9i8HlpSc",
	EnvGoogleCaptchaSecret:                  "YOU MUST CHANGE THIS",
	EnvPantahubUseCaptcha:                   "true",
	EnvPantahubJWTAuthSecret:                "YOU MUST CHANGE THIS",
	EnvPantahubJWTAuthPub:                   "YOU MUST CHANGE THIS",
	EnvPantahubJWESecret:                    "YOU MUST CHANGE THIS",
	EnvPantahubJWEPub:                       "YOU MUST CHANGE THIS",
	EnvPantahubScryptSecret:                 "YOU MUST CHANGE THIS",
	EnvPantahubJWTObjectSecret:              "YOU MUST CHANGE THIS",
	EnvPantahubJWTTimeoutMinutes:            "60",
	EnvPantahubRecoverJWTTimeoutMinutes:     "60",
	EnvPantahubJWTMaxRefreshMinutes:         "1440",

	EnvPantahubHost:       "localhost",
	EnvPantahubWWWHost:    "localhost",
	EnvPantahubPort:       "12365",
	EnvPantahubScheme:     "http",
	EnvPantahubAPIVersion: "", // XXX: needs to move to v0 at least for release

	EnvPantahubPortInt:    "12365",
	EnvPantahubPortIntTLS: "12366",

	// K8S info
	EnvK8SNamespace: "NA",

	// HOSTNAME
	EnvHostName: "localhost",

	// mailgun support for mail
	EnvMailgunAPIKey:    "",
	EnvMailgunDomain:    "",
	EnvMailgunPubAPIKey: "",

	EnvMongoHost:     "localhost",
	EnvMongoDb:       "pantabase-serv",
	EnvMongoUser:     "",
	EnvMongoPassword: "",
	EnvMongoPort:     "27017",
	EnvMongoRs:       "", // replicaSet; needed if connecting to multiple hosts

	// elastic search access
	EnvElasticURL:      "http://localhost:9200",
	EnvElasticUsername: "",
	EnvElasticPassword: "",
	EnvElasticBearer:   "",

	// fluent vars
	EnvFluentPort: "24224",

	// disable by default; to enable set this env...
	EnvFluentHost: "",

	// secrets - required!!
	EnvPantahubSaAdminSecret: "",
	EnvPantahubAdminSecret:   "",

	// roles/admins
	EnvPantahubAdmins:             "prn:pantahub.com:auth:/admin",
	EnvPantahubSubscriptionAdmins: "",

	// smtp config (we stopped supporting this in favour of mailgun for launch)
	EnvSMTPHost: "localhost",
	EnvSMTPPort: "25",
	EnvSMTPUser: "XXX",
	EnvSMTPPass: "XXX",
	EnvRegEmail: "Pantahub.com <team@pantacor.com>",

	// pantahub internal envs
	EnvPantahubAuth: "http://localhost:12365/auth",

	// pantahub www signup path
	EnvPantahubSignupPath: "/signup",

	// storage driver used to store objects
	EnvPantahubStorageDriver: "local",

	// access key of s3 storage credentials
	EnvPantahubS3AccessKeyID: "",

	// secret access key of s3 storage credentials
	EnvPantahubS3SecretAccessKeyID: "",

	// use anonymous credentials
	EnvPantahubS3SAnonymousCredentials: "true",

	// region where to store objects
	EnvPantahubS3Region: "us-east-1",

	// bucket where to store objects
	EnvPantahubS3Bucket: "pantahub",

	// enpoint of s3 server
	EnvPantahubS3Endpoint: "",

	// object storage path (when using "local" driver)
	EnvPantahubStoragePath: "/",

	// object storage path (deprecated, please use PANTAHUB_STORAGE_PATH)
	EnvPantahubS3Path: "../local-s3/",

	// resty REST client configs
	EnvRestyDebug: "",

	// Pantahub GC API end point
	EnvPantahubGCAPI: "http://localhost:2000",

	// Pantahub GC garbage removal flag
	EnvPantahubGCRemoveGarbage: "false",

	/* Pantahub GC UnClaimed expiry for device to mark it as garbage:
	   If a device is unclaimed for 5 Days then it will be marked as garbage

	   Format:ISO_8601: https://en.wikipedia.org/wiki/ISO_8601?oldformat=true#Durations
	*/
	EnvPantahubGCUnclaimedExpiry: "P5D", // => 5 Days

	/* Pantahub GC garbage expiry time to remove it:
	Once a device/trail/step/object is marked as
	garbage it will be removed after 2 days

	Format:ISO_8601: https://en.wikipedia.org/wiki/ISO_8601?oldformat=true#Durations
	*/
	EnvPantahubGCGarbageExpiry: "P2D", // => 2 Days

	// log requests,responses parameters and bodies
	EnvPantahubLogBody: "false",

	// Cron job timeout(seconds)
	EnvCronJobTimeout: "300",

	// Oauth CONFIGURATION
	EnvGoogleOAuthClientID:     "CHANGE THIS",
	EnvGoogleOAuthClientSecret: "CHANGE THIS",
	EnvGithubOAuthClientID:     "CHANGE THIS",
	EnvGithubOAuthClientSecret: "CHANGE THIS",
	EnvGitlabOAuthClientID:     "CHANGE THIS",
	EnvGitlabOAuthClientSecret: "CHANGE THIS",
}

// GetEnv get environment variable using variable key
func GetEnv(key string) string {
	v, f := os.LookupEnv(key)
	if !f {
		v = defaultEnvs[key]
	}

	return v
}
