Pantahub Base: the Pantacor Hub API, the backend that devices running
Pantavisor, the `pvr` tool, the Hub web app and Pantacor Fleet talk to.

# Prepare

 * Go 1.26.8, the release pinned in `go.mod` and in every Dockerfile
 * Docker with Compose, for the local stack and for the tests that start a
   throwaway MongoDB

The stack the API needs (MongoDB, Elasticsearch, fluentd, Kafka and the local
S3) is defined in `docker-compose.yml`; you do not have to install any of it by
hand.

# Build

```
$ go build -o ~/bin/pantahub-base .
```

# Run

The quickest way is the Compose stack, which builds the API from
`Dockerfile.development` with live reload:

```
$ docker compose up -d
```

The API then listens on http://localhost:12365 (and https on 12366). Objects go
to the `testing` bucket of the LocalStack S3 in the stack; create it once:

```
$ docker compose exec localstack awslocal s3 mb s3://testing
```

# Configure

The API is configured through environment variables. They are all declared,
with their defaults, in [`utils/env.go`](utils/env.go); `env.default` holds the
values the development stack uses.

# Test

```
$ go test ./...
```

Tests that need MongoDB either start their own with testcontainers (Docker must
be running) or read a connection string from the environment and are skipped
without it:

| Variable | Package |
| --- | --- |
| `PANTAHUB_DEVICES_TEST_MONGO` | `devices` |
| `PANTAHUB_MCP_TEST_MONGO` | `mcp` |
| `PANTAHUB_EXPORTS_TEST_MONGO` | `exports` |
| `PH_TEST_MONGO_URI` | `trails/trailmodels` |

Point them at a single-node replica set, for example:

```
$ docker run -d --rm --name ph-test-mongo -p 127.0.0.1:27099:27017 mongo:6.0 --replSet rs0
$ docker exec ph-test-mongo mongosh --quiet --eval 'rs.initiate({_id:"rs0",members:[{_id:0,host:"127.0.0.1:27017"}]})'
$ export PANTAHUB_DEVICES_TEST_MONGO="mongodb://127.0.0.1:27099/?directConnection=true"
```

`./security_check.sh` runs the same gosec and govulncheck scan as the CI's
security stage, which gates every build and deploy.

# APIs

 * [Auth](auth/README.md): accounts, logins, OAuth 2.1 and two-factor authentication
 * [Devices](devices/README.md)
 * [Trails](trails/README.md): revisions and their states
 * [Objects](objects/README.md)
 * [Logs](logs/README.md)
 * [Apps](apps/README.md): OAuth applications
 * [MCP](mcp/README.md): the Model Context Protocol endpoint for AI assistants
 * [Profiles](profiles/README.md)
 * [Subscriptions](subscriptions/README.md)
 * [Dash](dash/README.md)
 * [Callbacks](callbacks/README.md)
 * [Cron](cron/README.md)
 * [Healthz](healthz/README.md)
 * [Metrics](metrics/README.md)

`/exports`, `/tokens`, `/webhooks`, `/changes` and `/plog` are served as well,
and devices can use an MQTT message plane over WebSocket at `/mqtt/`.

# Branches, CI and releases

`master` is the default branch. Start every branch from it and open merge
requests against it.

| Event | CI |
| --- | --- |
| any push | the security scan (gosec, govulncheck) |
| merge to `master` | builds `registry.gitlab.com/pantacor/pantahub-base:master-<sha>` and deploys it to stage |
| a release tag `NNN` | builds `pantahub-base:NNN-<sha>` and deploys it to production |

A release is a `chore: update CHANGELOG.md for release NNN` commit on `master`,
tagged `NNN`. The changelog is generated with
[git-chglog](https://github.com/git-chglog/git-chglog):

```
$ git-chglog --next-tag NNN -o CHANGELOG.md
```

Pushing the tag is the production deploy; there is no separate step.

# Kubernetes

The Helm chart is in [`charts/pantahub`](charts/pantahub).

# PVR

The most convenient way to work with Pantahub for a subset of its features is
the `pvr` tool: https://gitlab.com/pantacor/pvr

# Issues/Support

Please use the issue tracker on GitLab.
