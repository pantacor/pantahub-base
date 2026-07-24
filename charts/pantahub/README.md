# pantahub (dev cluster chart)

Helm translation of the repo's `docker-compose.yml`: pantahub-base plus its
full backing stack, for spinning the whole dev environment up on a Kubernetes
cluster (k3s/k3d, minikube, kind, or a shared dev cluster).

```
helm install pantahub ./charts/pantahub -n pantahub --create-namespace
kubectl -n pantahub get pods -w
```

**New here? Read [GUIDE.md](GUIDE.md)** — step-by-step setup, access,
day-2 operations and troubleshooting. All images are public; no pull
secrets required.

## What maps to what

| compose service | chart resource | notes |
|---|---|---|
| `base` | Deployment `base` + Service + PVC `local-s3` | compose live-mounts the source into a `Dockerfile.development` build; here you deploy a built image (`base.image`/`base.tag`, default `registry.gitlab.com/pantacor/pantahub-base:develop`) |
| `gc`, `pvr`, `www`, `phs`, `fluentd` | Deployments (+ Services) | same images and env as compose |
| `mongo`, `mongo2`, `mongo3` | 3 Deployments + headless Services + PVCs | same names/ports as compose because `mongo-cluster-entrypoint` hardcodes the rs0 member list; Services are headless with `publishNotReadyAddresses` so members can self-identify in the replica set |
| `elasticsearch` | Deployment + PVC | `bootstrap.memory_lock` disabled (no memlock ulimits in k8s); a privileged initContainer sets `vm.max_map_count` (`elasticsearch.sysctlInit.enabled=false` to turn off) |
| `kibana` | Deployment (disabled by default) | compose keeps it behind the `logs` profile; enable with `--set kibana.enabled=true` |
| `zookeeper`, `kafka`, `kafka-schema-registry`, `kafka-rest`, `kafka-connect` | Deployments + Services | entrypoint scripts (topic creation, connector upload) shipped as ConfigMaps, same as the compose bind mounts |
| `localstack` | Deployment + Service | S3 only; the docker-socket/lambda options don't apply on k8s |
| `cronjobs` | 3 native CronJobs | the compose container emulated k8s cronjobs with crond; here they are the real thing |
| `nginx` | optional Ingress (`ingress.enabled=true`) | set `ingress.domain` to serve `api./hub./pvr.<domain>` publicly with Let's Encrypt TLS via cert-manager (see GUIDE.md §4b); the mTLS client-cert proxy part is not translated |

`env.default` + `.env.local` become the `pantahub-env` ConfigMap, rendered
from `.Values.env`. Override the same way you'd edit `.env.local`:

```yaml
# my-values.yaml
env:
  PANTAHUB_SA_ADMIN_SECRET: "changeme"
  PANTAHUB_DEMOACCOUNTS_PASSWORD_admin: "changeme"
```

## Startup ordering

Kubernetes has no `depends_on`; ordering is handled the same way the compose
entrypoints already did it — wait loops:

- `base` has an initContainer waiting for mongo, fluentd and elasticsearch
  (base treats fluentd as a hard startup dependency).
- `kafka-connect` waits for kafka, then uploads the connector configs.
- `phs` waits for the `pantabase` debezium connector and its Avro schemas.

Expect a few restart cycles during the first minutes of bring-up; that's
normal and self-heals.

## Kubernetes-specific adaptations

Differences from compose that the chart handles for you (all found by
actually running it — don't undo them):

- `enableServiceLinks: false` on every pod: k8s injects `<SERVICE>_PORT` env
  vars that crash the Confluent images ("port is deprecated").
- base runs with `ENVFILE=/dev/null` and gc gets an empty file mounted over
  `/opt/ph/bin/env.default` — both images otherwise source their baked
  `env.default` on startup, clobbering the ConfigMap env.
- The JWT/JWE defaults in `values.yaml` are plain base64 PEM (env.default's
  `REMOVE==FOR==TESTING…:::` warning prefix stripped — the app doesn't parse
  it), and `MONGO_DB` defaults to `pantabase-serv` to match the users the
  mongo entrypoint creates and the Debezium whitelist.
- mongo Services are headless with `publishNotReadyAddresses: true` (see
  table above).

## Caveats

- **Fixed service names** (`mongo`, `kafka`, `base`, ...): one release per
  namespace, and the names must not change — entrypoints and connector configs
  reference them.
- **Dev secrets**: every credential is a value, each defined in exactly one
  place and rendered wherever it's needed (mongo entrypoint, connector
  configs, kibana.yml):
  - JWT/JWE keys, scrypt secret, app mongo user — `env.*` (`MONGO_USER`,
    `MONGO_PASS`, `MONGO_DB`)
  - mongo root user — `mongo.env.MONGO_INITDB_ROOT_USERNAME`/`_PASSWORD`
  - kafka-connect mongo users — `mongo.connectUsers.source`/`.sink`
  - mongo replica-set keyfile — `mongo.keyfileKeys`
  - kibana xpack encryption keys — `kibana.encryptionKeys.*`

  The defaults are the well-known dev values; replace all of them (and move
  the ConfigMap-rendered ones to a Secret) before exposing a cluster to
  anything real. Mongo passwords are embedded in `mongodb://` URIs — keep
  them URL-safe.
- The `files/` directory contains **copies** of `entrypoints/`,
  `fluentd.localhost.conf` and `kafka/connect-configs/` — if you change the
  originals, re-copy them (or wire that into CI). They are rendered through
  `tpl`, so `files/kibana.yml`, `files/entrypoints/mongo-cluster-entrypoint`
  and `files/connect-configs/*` intentionally diverge from the originals
  where credentials were hardcoded: those come from values (see above).
- Confluent 5.4.3 images assume generous `nofile` ulimits; kubelet defaults
  are normally fine, but raise them on the node if kafka complains.
