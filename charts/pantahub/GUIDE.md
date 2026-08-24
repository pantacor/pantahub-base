# Pantahub dev cluster — usage guide

Step-by-step instructions for running the full pantahub stack (the
`docker-compose.yml` equivalent) on Kubernetes with this chart. For the
compose-to-chart mapping and design notes, see [README.md](README.md).

All images are public — no registry credentials or pull secrets are needed.

## 1. Prerequisites

- `kubectl` and `helm` v3 on your machine
- A Kubernetes cluster with ~6 GB of RAM to spare and a default StorageClass
  (7 PVCs are created). Any of these work:

```bash
# k3d (k3s in docker) — ships local-path storage out of the box
k3d cluster create pantahub --agents 1

# or minikube
minikube start --memory 8192 --cpus 4

# or kind
kind create cluster --name pantahub
```

## 2. Configure

Create `my-values.yaml` — this plays the role of `.env.local` in the compose
setup. Minimal useful example:

```yaml
env:
  # basic-auth secret for the cron endpoints (username saadmin)
  PANTAHUB_SA_ADMIN_SECRET: "changeme"
  # demo account passwords (see note below)
  PANTAHUB_DEMOACCOUNTS_PASSWORD_admin: "changeme"
```

**Demo accounts**: base fails closed. Unless `PANTAHUB_PRODUCTION` is
explicitly `"false"`, it runs in production mode: every built-in demo account
(`admin`, `user1`–`user3`, …) is disabled except those given an explicit
`PANTAHUB_DEMOACCOUNTS_PASSWORD_*`. Leaving `PANTAHUB_PRODUCTION` unset is
the same as production. Only set `PANTAHUB_PRODUCTION: "false"` on a private
development cluster — that enables ALL demo accounts with their code defaults,
including `admin`/`admin`.

Everything under `env:` is merged over the defaults in `values.yaml` (which
mirror `env.default`, including the **dev-only** JWT/JWE keys — replace those
for any cluster other people can reach).

All other credentials are plain values too, each defined once and rendered
everywhere it's used (mongo user creation, kafka-connect connector configs,
kibana.yml — see the "Dev secrets" caveat in README.md for the full list):

```yaml
env:
  MONGO_USER: "user"         # app db user (also created in mongo)
  MONGO_PASS: "pass"
mongo:
  env:
    MONGO_INITDB_ROOT_USERNAME: "admin"
    MONGO_INITDB_ROOT_PASSWORD: "admin"
  connectUsers:              # kafka-connect mongo users
    source: {username: "phskafkaconnectsource", password: "pass"}
    sink: {username: "phskafkaconnectsink", password: "pass"}
  keyfileKeys:               # replica-set keyfile (openssl rand -base64 32)
    - "62PQfye4Pdg89I45jcB/GxHNcoaGgLXP1ZOSh4QLttM="
kibana:
  encryptionKeys:            # xpack keys (openssl rand -hex 16)
    encryptedSavedObjects: "..."
    reporting: "..."
    security: "..."
```

Mongo users are created on **first** bring-up; changing passwords later does
not update existing users in the data volume. Mongo passwords end up inside
`mongodb://` URIs — keep them URL-safe.

Other switches you may want:

```yaml
kibana:
  enabled: true            # log UI (compose "logs" profile)

base:
  tag: mybranch            # any pantahub-base image tag

www:
  service:
    type: NodePort         # reach the UI without port-forward
```

## 3. Install

```bash
helm install pantahub ./charts/pantahub -n pantahub \
  --create-namespace -f my-values.yaml
```

Watch it come up:

```bash
kubectl -n pantahub get pods -w
```

**First bring-up takes several minutes** and follows this order:

1. `zookeeper`, `mongo/mongo2/mongo3`, `elasticsearch`, `fluentd`, `localstack`
2. `mongo` initializes replica set `rs0` and creates the app users
3. `kafka` starts and creates the topics
4. `kafka-schema-registry`, `kafka-rest`
5. `kafka-connect` becomes ready, then uploads the Debezium connector configs
6. `phs` starts once the `pantabase` connector and its Avro schemas exist
7. `base` starts after mongo/fluentd/elasticsearch respond (initContainer),
   then `gc`, `pvr`, `www`

A few `CrashLoopBackOff`/restarts on the kafka side during the first minutes
are normal — the wait-loops in the entrypoints sort themselves out.

Sanity check when everything is Ready:

```bash
kubectl -n pantahub run -it --rm check --image=curlimages/curl --restart=Never \
  -- curl -s http://base:12365/
```

## 4. Access the services

Same ports the compose file publishes, via port-forward:

| what | command | open |
|---|---|---|
| API (base) | `kubectl -n pantahub port-forward svc/base 12365:12365` | http://localhost:12365 |
| Web UI (www) | `kubectl -n pantahub port-forward svc/www 3000:80` | http://localhost:3000 |
| pvr | `kubectl -n pantahub port-forward svc/pvr 12367:12367` | http://localhost:12367 |
| kibana (if enabled) | `kubectl -n pantahub port-forward svc/kibana 5601:5601` | http://localhost:5601 |
| mongo primary | `kubectl -n pantahub port-forward svc/mongo 27017:27017` | `mongodb://user:pass@localhost:27017` |
| elasticsearch | `kubectl -n pantahub port-forward svc/elasticsearch 9200:9200` | http://localhost:9200 |

The `www` defaults (`REACT_APP_API_URL=http://localhost:12365`, etc.) assume
exactly these forwards, so the UI works out of the box with the first three
running.

For a cluster serving real clients, skip port-forwarding and go public with a
domain — next section.

## 4b. Go public: domain + Let's Encrypt TLS

The client-facing interfaces — the API (`base`), the web UI (`www`) and
`pvr` — can be served on public subdomains with TLS certificates from
Let's Encrypt:

- `api.<domain>` → base
- `hub.<domain>` → www
- `pvr.<domain>` → pvr

**Prerequisites** (once per cluster):

```bash
# an ingress controller, if the cluster doesn't have one (k3s ships traefik)
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm install ingress-nginx ingress-nginx/ingress-nginx \
  -n ingress-nginx --create-namespace

# cert-manager, which requests/renews the Let's Encrypt certificates
helm repo add jetstack https://charts.jetstack.io
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true
```

On stock **k3s** you can skip the ingress-nginx install — its bundled traefik
is the default IngressClass and the chart uses the cluster default when
`ingress.className` is left empty.

**DNS**: point `api.`, `hub.` and `pvr.<domain>` (or a wildcard) at the
ingress controller's external IP (`kubectl -n ingress-nginx get svc`, or the
node's public IP on single-node k3s). Let's Encrypt HTTP-01 validation
requires the names to resolve publicly and port 80 to be reachable — with DNS
pointed *before* the install, issuance typically completes in under a minute.

**Values**:

```yaml
ingress:
  enabled: true
  # className: nginx        # only needed when not using the cluster default
  domain: pantahub.example.com
  letsencrypt:
    email: you@example.com  # ACME account email (expiry notices)
```

Then `helm upgrade ... -f my-values.yaml`. Enabling the ingress also rewires
the app automatically — `PANTAHUB_HOST`, `PANTAHUB_SCHEME`,
`PANTAHUB_HOST_WWW`, `PH_AUTH` and the UI's `REACT_APP_*` URLs all switch to
`https://api./hub./pvr.<domain>`, and base/gc/www roll to pick that up.

(Without `domain`, the ingress serves the `.localhost` fallback hosts —
useful on a local cluster with 80/443 mapped; set `ingress.tls.enabled=false`
and `ingress.letsencrypt.enabled=false` there, Let's Encrypt can't issue for
`.localhost` names.)

The chart creates the `letsencrypt-prod` ClusterIssuer for you
(`ingress.letsencrypt.createIssuer=false` if your cluster already has one —
then just set `ingress.letsencrypt.issuer` to its name). The certificate for
all three hosts lands in the `pantahub-tls` secret; check progress with:

```bash
kubectl -n pantahub get certificate,order,challenge
```

While testing, use the Let's Encrypt staging endpoint to avoid rate limits:

```yaml
ingress:
  letsencrypt:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
```

Remember the defaults ship dev JWT/JWE keys and open demo accounts — replace
them (step 2) **before** exposing the cluster publicly.

## 5. Day-2 operations

**Change config / env** — edit `my-values.yaml`, then:

```bash
helm upgrade pantahub ./charts/pantahub -n pantahub -f my-values.yaml
```

`base` and `gc` roll automatically when `env:` changes (checksum annotation).

**Bump an image** — change the tag in `my-values.yaml` (e.g. `base.tag`) and
`helm upgrade`. If the tag didn't change but the image did (moving tags like
`develop`), force a re-pull with:

```bash
kubectl -n pantahub rollout restart deploy/base
```

**Edited an entrypoint / fluentd conf / connector config in the repo?**
The chart ships copies under `charts/pantahub/files/`. The ones that are
verbatim copies can simply be re-copied:

```bash
cp entrypoints/{mongo-cluster-secondary-entrypoint,kafka-entrypoint,kafka-connect-entrypoint,phs-entrypoint} charts/pantahub/files/entrypoints/
cp fluentd.localhost.conf charts/pantahub/files/fluentd/fluent.conf
helm upgrade pantahub ./charts/pantahub -n pantahub -f my-values.yaml
```

**Do NOT blindly copy** `mongo-cluster-entrypoint`, `kafka/connect-configs/*.json`
or `entrypoints/kibana.yml` — their chart copies are templated (credentials
come from values; see the "Dev secrets" caveat in README.md) and a plain `cp`
would wipe that. Port changes to those by hand, e.g.:

```bash
diff entrypoints/mongo-cluster-entrypoint charts/pantahub/files/entrypoints/mongo-cluster-entrypoint
```

**Run a cron endpoint by hand** (instead of waiting for the CronJob):

```bash
kubectl -n pantahub create job --from=cronjob/pantahub-publicdevices manual-run-1
```

## 6. Reset / uninstall

```bash
helm uninstall pantahub -n pantahub
```

PVCs (and therefore all data: mongo, elasticsearch, kafka, object storage)
survive uninstall on purpose. For a full wipe:

```bash
kubectl -n pantahub delete pvc --all
# or just nuke the namespace
kubectl delete namespace pantahub
```

Deleting only `data-kafka` or only `zookeeper` state can cause cluster-id
mismatch pain — the kafka entrypoint already clears `meta.properties` to
handle that, but when in doubt wipe both together.

## 7. Smoke test

`smoke-test.sh` (next to this file) runs the whole cycle unattended: creates
a throwaway k3d cluster (own kubeconfig — your `~/.kube/config` is not
touched), installs the chart, waits for every deployment, probes
base/www/pvr/gc/elasticsearch from inside the cluster, verifies the mongo
replica set initialized, and deletes the cluster again:

```bash
cd charts/pantahub
./smoke-test.sh                        # needs docker, k3d, kubectl, helm
KEEP=true ./smoke-test.sh              # keep the cluster to poke around
VALUES=overlay.yaml ./smoke-test.sh    # test with extra values
```

**Architecture matters**: the pantacor and confluent images are amd64-only,
so the full smoke test needs an amd64 host (a dev box or a CI runner — the
script is CI-friendly, exits non-zero on failure). On an arm64 machine you
can still smoke-test the chart mechanics with the arm64-capable subset
(mongo replica set, elasticsearch, localstack) by disabling the rest:

```yaml
# smoke-arm64.yaml
base: {enabled: false}
gc: {enabled: false}
pvr: {enabled: false}
www: {enabled: false}
phs: {enabled: false}
fluentd: {enabled: false}
zookeeper: {enabled: false}
kafka: {enabled: false}
kafkaSchemaRegistry: {enabled: false}
kafkaConnect: {enabled: false}
kafkaRest: {enabled: false}
cronjobs: {enabled: false}
```

```bash
VALUES=smoke-arm64.yaml ./smoke-test.sh
```

**Full end-to-end on a throwaway cloud VM** (what was used to validate this
chart, including the public domain + Let's Encrypt flow): create an amd64 VM
(e.g. a DigitalOcean droplet, 16 GB recommended), install k3s and helm,
point `api./hub./pvr.<domain>` DNS at its IP, then:

```bash
curl -sfL https://get.k3s.io | sh -            # k3s incl. traefik ingress
helm install cert-manager jetstack/cert-manager -n cert-manager \
  --create-namespace --set crds.enabled=true
helm install pantahub ./pantahub -n pantahub --create-namespace \
  --set ingress.enabled=true --set ingress.domain=<domain> \
  --set ingress.letsencrypt.email=<you@example.com> \
  --set env.PANTAHUB_SA_ADMIN_SECRET=$(openssl rand -hex 16)
```

Delete the VM when done — everything it hosted is reproducible from the
chart.

## 8. Troubleshooting

| symptom | likely cause / fix |
|---|---|
| `ImagePullBackOff` | typo'd or nonexistent image tag, or the node can't reach the registry — `kubectl -n pantahub describe pod <pod>` shows the exact pull error |
| PVCs stuck `Pending` | no default StorageClass — set `global.storageClass` or install one (kind: `local-path-provisioner`) |
| elasticsearch OOM/crash with `max_map_count` error | the privileged sysctl initContainer is disabled or forbidden — allow it, or set `vm.max_map_count=262144` on the node yourself |
| `phs` stuck logging "pantabase connector not ready" | that's its wait-loop; check `kafka-connect` is Ready and its log shows the connector uploads (`kubectl -n pantahub logs deploy/kafka-connect`) |
| `base` init stuck on "waiting for mongo" | mongo pods not Ready — check `kubectl -n pantahub logs deploy/mongo` for replica-set init errors |
| `base` crashes: "no valid JWT secret … base64" | `PANTAHUB_JWT_SECRET`/`_PUB`/`_JWE_*` overridden with a non-base64 value — they must be base64-encoded PEM (see `values.yaml` comments) |
| `base` crashes: mongo `AuthenticationFailed` | `MONGO_DB`/`MONGO_USER`/`MONGO_PASS` don't match the users the mongo entrypoint creates (defaults: `pantabase-serv`/`user`/`pass`) |
| env overrides seem ignored by base/gc | they were being clobbered by the image's baked `env.default` — the chart neutralizes that (base: `ENVFILE=/dev/null`, gc: empty file mounted over it); don't remove those pieces |
| confluent pods crash: "port is deprecated" | kubernetes service-link env vars (`KAFKA_PORT`, …) leaked into the pod — the chart sets `enableServiceLinks: false` everywhere; keep it that way |
| mongo member comes up REMOVED after restart | its Service must stay headless with `publishNotReadyAddresses: true` (chart default) so a starting member can resolve itself in the stored rs config |
| cron endpoints return 401 | `PANTAHUB_SA_ADMIN_SECRET` empty — set it in `my-values.yaml` (basic auth user is `saadmin`) |
| fluentd OOMKilled under log load | raise `fluentd.resources.limits.memory`; keep buffers bounded in `files/fluentd/fluent.conf` |
| in-cluster DNS times out in local k3d/kind | some hosts (e.g. ChromeOS/Crostini kernels) break pod-to-pod UDP — not a chart problem; run the smoke test on a normal amd64 host or a cheap cloud VM instead |
