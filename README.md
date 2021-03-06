
Pantahub Base APIs reference implementation.

# Prepare

 * get a reasonable fresh golang engine (1.9++) and install it
 * Install a mongodb database locally or get credentials for hosted instance
 * Install elasticsearch 6.x.x and start it using default settings
 * Install fluentd or td-agent (on windows) and start it using with the config
   include in pantahub-base source: fluentd.localhost.conf
 * Decide where you want to store the objects. By default we store objects in
   $CWD/../local-s3/ folder; you can use environment variables (see below)
   to adjust this

# Build

```
$ go get -v u gitlab.com/pantacor/pantahub-base
...

$ go build -o ~/bin/pantahub-base gitlab.com/pantacor/pantahub-base
...
``` 
# Test

* Note:Make Sure testharness project is accessible

```
$ git clone -b develop https://gitlab.com/pantacor/pantahub-testharness
...

```
$ go test -v ./tests/...
...

# Run

```
$ pantahub-base
mongodb connect: mongodb://localhost:27017/pantabase-serv
S3 Development Path: ../local-s3/
2017/06/19 21:56:04 Serving @ https://127.0.0.1:12366/
2017/06/19 21:56:04 Serving @ http://127.0.0.1:12365/
2017/06/19 21:56:04 Serving @ https://::1:12366/
2017/06/19 21:56:04 Serving @ http://::1:12365/
2017/06/19 21:56:04 Serving @ https://10.42.0.1:12366/
2017/06/19 21:56:04 Serving @ http://10.42.0.1:12365/
2017/06/19 21:56:04 Serving @ https://fe80::90a9:7a0:a5d5:f808:12366/
2017/06/19 21:56:04 Serving @ http://fe80::90a9:7a0:a5d5:f808:12365/
2017/06/19 21:56:04 Serving @ https://192.168.178.75:12366/
2017/06/19 21:56:04 Serving @ http://192.168.178.75:12365/
2017/06/19 21:56:04 Serving @ https://2a02:2028:66c:1201:3bae:315f:3ad8:c6ee:12366/
2017/06/19 21:56:04 Serving @ http://2a02:2028:66c:1201:3bae:315f:3ad8:c6ee:12365/
2017/06/19 21:56:04 Serving @ https://fe80::f64a:6b7d:ede:b208:12366/
2017/06/19 21:56:04 Serving @ http://fe80::f64a:6b7d:ede:b208:12365/
2017/06/19 21:56:04 Serving @ https://172.18.0.1:12366/
2017/06/19 21:56:04 Serving @ http://172.18.0.1:12365/
2017/06/19 21:56:04 Serving @ https://fe80::42:82ff:fea9:63a4:12366/
2017/06/19 21:56:04 Serving @ http://fe80::42:82ff:fea9:63a4:12365/
2017/06/19 21:56:04 Serving @ https://172.17.0.1:12366/
2017/06/19 21:56:04 Serving @ http://172.17.0.1:12365/
2017/06/19 21:56:04 Serving @ https://fe80::42:97ff:fef7:9daa:12366/
2017/06/19 21:56:04 Serving @ http://fe80::42:97ff:fef7:9daa:12365/
2017/06/19 21:56:04 Serving @ https://fe80::5491:a3ff:fed7:c798:12366/
2017/06/19 21:56:04 Serving @ http://fe80::5491:a3ff:fed7:c798:12365/
2017/06/19 21:56:04 Serving @ https://fe80::c0a3:b4ff:fe0d:e3b8:12366/
2017/06/19 21:56:04 Serving @ http://fe80::c0a3:b4ff:fe0d:e3b8:12365/
```

# Configure

We currently support the environment variables you can find in utils/env.go:

```
const (
	// Pantahub JWT Secret. THIS MUST BE SET TO SOMETHING SECRET!!
	// default: "THIS MUST BE CHANGED"
	EnvPantahubJWTAuthSecret = "PANTAHUB_JWT_SECRET"

	// Host you want clients to reach this server under
	// default: localhost
	EnvPantahubHost       = "PANTAHUB_HOST"

	// Port you want to make this server available under
	// default: 12365 for http and 12366 for https
	EnvPantahubPort       = "PANTAHUB_PORT"

	// Default scheme to use for urls pointing at this server when we encode
	// them in json or redirect (e.g. for auth)
	// default: http
	EnvPantahubScheme     = "PANTAHUB_SCHEME"

	// XXX: not used
	EnvPantahubAPIVersion = "PANTAHUB_APIVERSION"

	// Authentication endpoint to point clients to that need access tokens
	// or need more privileged access tokens.
	// default: $PANTAHUB_SCHEME://$PANTAHUB_HOST:$PANTAHUB_PORT/auth
	EnvPantahubAuth       = "PH_AUTH"

	// port to listen to on for http on internal interfaces
	// default: 12365
	EnvPantahubPortInt     = "PANTAHUB_PORT_INT"

	// port to listen to on for https on internal interfaces
	// default: 12366
	EnvPantahubPortIntTLS = "PANTAHUB_PORT_INT_TLS"

	// Hostname for mongodb connection
	// default: localhost
	EnvMongoHost          = "MONGO_HOST"

	// Port for mongodb connection
	// default: 27017
	EnvMongoPort          = "MONGO_PORT"

	// Database name for mongodb connection
	// default: pantabase-serv
	EnvMongoDb            = "MONGO_DB"

	// Database user for mongodb connection
	// default: <none>
	EnvMongoUser          = "MONGO_USER"

	// Database password for mongodb connection
	// default: <none>
	EnvMongoPassword          = "MONGO_PASS"

	// SMTP host to use for sending mails
	// default: <none>
	EnvSMTPHost           = "SMTP_HOST"

	// SMTP port to use for sending mails
	// default: <none>
	EnvSMTPPort           = "SMTP_PORT"

	// SMTP user to use for sending mails
	// default: <none>
	EnvSMTPUser           = "SMTP_USER"

	// SMTP pass to use for sending mails
	// default: <none>
	EnvSMTPPass           = "SMTP_PASS"
)
```

# APIs

The following APIs are currently included and documented:

 * [Auth API](auth/README.md)
 * [Devices API](devices/README.md)
 * [Trails API](trails/README.md)
 * [Logs API](logs/README.md)


# PVR

The most convenient way to interface with pantahub for a subset of its features is through the ```pvr``` tool.

See https://gitlab.com/pantacor/pvr for more features.

# Docker

Convenience docker builds are available in gcr.io/pantahub-registry/pantahub-base

To run the latest:

```
docker run -it --rm \
	-v/path/to/storage:/opt/ph/local-s3 \
	gcr.io/pantahub-registry/pantahub-base:latest
```

# Build your own Docker

Want to build your own docker images? Check out https://gitlab.com/pantacor/pantahub-containers/
and the readmes there

# Docker Compose

Get start with `docker-compose`:

```bash
$ docker-compose up -d
```

Create the test local s3 bucket

```bash
$ ./locals3.bash mb s3://testing/
make_bucket: testing
```

Run all tests

```bash
$ docker-compose exec base go test ./...
```

# Kubernetes

Check our example deployment manifest in https://gitlab.com/pantacor/pantahub-containers/api/k8s directory.

# Issues/Support:

Please use Issue trackers on gitlab.

