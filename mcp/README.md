# MCP endpoint

A [Model Context Protocol](https://modelcontextprotocol.io) server that lets an
AI assistant (claude.ai, Claude Code, OpenCode, MCP Inspector, ...) look after
the devices of the user who authorized it: see devices, revisions and logs,
change configuration, and build and deploy new revisions the way the web app's
revision editor (pvtx) does. It is served by the API itself, at `/mcp`, over
Streamable HTTP in stateless mode.

- [Configuration](#configuration)
- [Connecting a client](#connecting-a-client)
- [Authentication and scopes](#authentication-and-scopes)
- [Tools](#tools)
- [Changing revisions](#changing-revisions)
- [Exports: download, upload, import](#exports-download-upload-import)
- [Security model](#security-model)
- [Tests](#tests)

## Configuration

The endpoint:

| Variable | Default | |
|---|---|---|
| `PANTAHUB_MCP_ENABLED` | `false` | serve the endpoint |
| `PANTAHUB_MCP_PATH` | `/mcp` | path it is mounted under |
| `PANTAHUB_MCP_RESOURCE_URL` | from `PANTAHUB_SCHEME`/`HOST`/`PORT` | public URL of the endpoint. It is the OAuth resource identifier: set it to exactly what users type into their MCP client when the API sits behind a tunnel or proxy |
| `PANTAHUB_MCP_ALLOW_API_TOKENS` | `false` | development only: accept ordinary login tokens, which carry no audience |

How clients sign in (the authorization server lives in `auth/`; see "Standard
OAuth 2.1 clients" in `auth/README.md`):

| Variable | Default | |
|---|---|---|
| `PANTAHUB_OAUTH_CIMD_ENABLED` | `false` | accept clients identified by a metadata document URL, such as claude.ai and Claude Code |
| `PANTAHUB_OAUTH_CIMD_ALLOWED_HOSTS` | empty (any public host) | hosts those URLs may live on, e.g. `claude.ai` |
| `PANTAHUB_OAUTH_DCR_ALLOWED_REDIRECT_HOSTS` | empty (DCR off) | hosts a dynamically registered client may redirect to, e.g. `claude.ai,127.0.0.1,localhost`; the loopback entries admit desktop clients |
| `PANTAHUB_OAUTH_REFRESH_TOKEN_DAYS` | `30` | how long an unused connection lasts |

Exports (the `exports/` package; the MCP tools use it):

| Variable | Default | |
|---|---|---|
| `PANTAHUB_EXPORT_UPLOAD_MAX_BYTES` | 2 GiB | largest export archive taken in, compressed |
| `PANTAHUB_EXPORT_IMPORT_ALLOWED_HOSTS` | empty (off) | hosts `import_export_from_url` may fetch from, e.g. `gitlab.com,storage.googleapis.com` for CI artifacts and the storage they redirect to |

Stage runs with `PANTAHUB_MCP_ENABLED=true`,
`PANTAHUB_OAUTH_CIMD_ENABLED=true`, `PANTAHUB_OAUTH_CIMD_ALLOWED_HOSTS=claude.ai`
and `PANTAHUB_OAUTH_DCR_ALLOWED_REDIRECT_HOSTS=claude.ai,127.0.0.1,localhost`.

## Connecting a client

A client only needs the endpoint URL. It discovers the authorization server
from the 401 challenge, sends the user to the consent page of the web app, and
receives a token bound to this endpoint plus a refresh token.

- **claude.ai**: Settings > Connectors > Add custom connector, and enter
  `https://<api host>/mcp`. Requests come from Anthropic's cloud, so the API has
  to be reachable from the internet over HTTPS.
- **Claude Code**: `claude mcp add --transport http pantahub https://<api host>/mcp`
  (add `--scope user` for every project), then `/mcp` to sign in.
- **OpenCode**: add a remote server in `~/.config/opencode/config.json`, then
  `opencode mcp auth pantahub`.
- **MCP Inspector**: `npx @modelcontextprotocol/inspector --transport http --server-url https://<api host>/mcp`

Each consent is a *connection* the user sees in the web app (Security >
Connected applications) and can end at any time; the endpoint stops accepting
that connection's tokens within 15 seconds.

For local work without the OAuth round trip, `PANTAHUB_MCP_ALLOW_API_TOKENS=true`
accepts a login token:

```sh
TOKEN=$(curl -s localhost:12365/auth/login -A curl -d '{"username":"user1","password":"user1"}' | jq -r .token)
claude mcp add --transport http pantahub http://localhost:12365/mcp \
  --header "Authorization: Bearer $TOKEN"
```

## Authentication and scopes

Every request needs `Authorization: Bearer <token>`: RS256, signed with the API
key, of a `USER` or `SESSION` account, with `aud` the endpoint URL, `iss` this
API, and `cnx` a connection that still stands. A token for another audience is
always refused; a token without audience only with
`PANTAHUB_MCP_ALLOW_API_TOKENS=true`. Tokens bound to this endpoint are refused
everywhere else: the REST API and MQTT reject any token whose audience is a URL.

A missing or invalid token gets `401` with
`WWW-Authenticate: Bearer resource_metadata="..."`, pointing at the RFC 9728
document at `/.well-known/oauth-protected-resource/mcp`.

A first connection is asked for read access only (`devices.readonly`,
`trails.readonly`). A tool call the token does not cover gets `403` with
`WWW-Authenticate: Bearer error="insufficient_scope", scope="..."`, and the
client asks the user for that scope then (step-up). Any one scope in a row
unlocks the tools:

| Tools | Scopes |
|---|---|
| `list_devices`, `get_device`, `get_device_logs`, `list_device_tokens`, `get_device_token`, `get_export_link` | `devices.readonly`, `devices`, `all.readonly`, `all` |
| `get_device_status`, `list_revisions`, `get_revision`, `get_revision_parts`, `get_export_upload` | `trails.readonly`, `trails`, `all.readonly`, `all` |
| `update_user_meta` | `devices.write`, `devices`, `all` |
| `update_device_token` | `devices.change`, `devices`, `all` |
| `plan_revision`, `commit_revision`, `get_export_upload_link`, `import_export_from_url` | `trails.write`, `trails`, `all` |
| `list_apps`, `get_app` | `apps.readonly`, `all.readonly`, `all` |
| `update_app` | `apps.write`, `all` |

A token issued for this endpoint only ever carries the narrow scopes
(`devices.readonly`, `trails.readonly`, `devices.write`, `devices.change`,
`trails.write`, `apps.readonly`, `apps.write`); the catch-all rows only matter
with `PANTAHUB_MCP_ALLOW_API_TOKENS`.

## Tools

Every tool takes a device by id or nick, and only ever sees the devices owned
by the account in the token: somebody else's device looks exactly like one that
does not exist. Tools that change something are annotated as destructive, so a
client asks the user before each call.

**Devices and revisions (read)**

| Tool | |
|---|---|
| `list_devices` | the account's devices, paged, optionally by nick prefix |
| `get_device` | one device with its device-meta (reported by the device) and user-meta (set by the owner, global profile meta included) |
| `get_device_status` | whether the device runs its newest revision, and how far it got with it |
| `list_revisions` | a device's revisions, newest first |
| `get_revision` | one revision: status, progress log, files, optionally the whole state |
| `get_revision_parts` | a revision part by part: apps and BSP with their files, configuration overlays, documents, and each signature with what it protects |
| `get_device_logs` | a device's logs, filtered by revision, level, source and time |

**Configuration**

| Tool | |
|---|---|
| `update_user_meta` | set and remove user-meta keys of a device; the device picks the change up on its own |
| `list_device_tokens`, `get_device_token`, `update_device_token` | device join tokens (rename, change the user-meta enrolled devices start with), never their secrets |
| `list_apps`, `get_app`, `update_app` | the account's OAuth applications (name and callback URLs only), never their secrets |

**Revisions and exports (change)**

| Tool | |
|---|---|
| `plan_revision` | prepare a new revision without sending anything; see below |
| `commit_revision` | send a planned revision to the device |
| `get_export_link` | a ten-minute download link to a revision's pvr export |
| `get_export_upload_link` | a single-use link to upload one pvr export |
| `import_export_from_url` | have the server fetch a pvr export (a CI artifact) |
| `get_export_upload` | how an upload or import went, and what it holds |

Join tokens, applications and devices are never created or deleted here, and
no secret is ever returned: a join token or confidential application yields a
secret shown once, and through this endpoint it would be shown to an assistant
and kept in its conversation. That stays in the web app.

## Changing revisions

A revision is changed in two steps, so that what the user approves is exactly
what the device is sent.

1. **`plan_revision(device, operations)`** starts from the device's newest
   revision, applies the operations in order, and stores the resulting state as
   a plan for an hour. It answers the plan id, the new revision number, every
   added, removed and changed file, the parts touched, and warnings.
2. **`commit_revision(plan_id, message)`** posts exactly that state as the next
   revision, through the same code as `POST /trails/:id/steps`. A plan is
   committed once. If the device got another revision since the plan was made,
   the commit fails and nothing is posted: plan again.

The device then installs it; follow with `get_device_status` and, when it ends
in `WONTGO` or `ERROR`, `get_device_logs`.

Operations (`trails/stateops`, which does the merging with pvr's own libpvr, so
the result is the state pvr would have produced):

| `op` | Arguments | |
|---|---|---|
| `remove_parts` | `parts` | remove apps or signatures; an app goes with its `_config/<app>` overlay and its signature, and a signature takes the files only it protects |
| `copy_parts` | `parts`, `from_device` and/or `from_revision` | take parts from another device or an older revision, replacing the ones of that name, with the signatures that cover them |
| `rollback` | `from_revision`, optional `parts` | go back to an older revision entirely, or for some parts only |
| `set_document` | `path`, `value` | replace an inline JSON document such as `<app>/run.json`; binaries cannot be edited, only copied with their part |
| `delete_file` | `path` | remove one file |
| `import_export` | `upload_id` | merge a received pvr export in, as the web app's editor does: every part it brings replaces the part of that name, with its signature |

Revisions posted this way carry `source: mcp`, the plan id and the OAuth client
in their meta, and the commit message in their history.

### Signatures

Signatures are made with pvr (`pvr sig add`, `pvr sig update`) or in CI, and
verified by Pantavisor on the device. Nothing here makes or checks one: signing
keys never reach the Hub, so a compromised Hub or a stolen connection still
cannot produce a state a verifying device accepts.

What the plan does is tell where a device that verifies signatures is likely to
refuse the result, from which files each signature protects (the same
selection a device makes, through libpvr): a signature left unchanged while
files it protects changed, a signature that cannot be read, and parts nothing
signs any more on a device whose state was signed. Parts signed without their
configuration (`pvr sig add --noconfig`) can have their configuration edited
without breaking the signature.

## Exports: download, upload, import

A pvr export is what `pvr export` writes: a `.tar.gz` with the state as `json`
and every object as `objects/<sha256>`.

- **Download**: `get_export_link(device, revision?, parts?)` returns a link to
  `GET /exports/links/<token>/<nick>-<rev>.tar.gz` that works without signing
  in for ten minutes: the same archive `GET /exports/:owner/:nick/:rev/:file`
  gives a logged-in owner.
- **Upload**: `get_export_upload_link()` returns a single-use URL valid for
  thirty minutes (`PUT /exports/uploads/<token>`):
  `curl -T export.tar.gz '<upload_url>'`.
- **Import from a URL**: `import_export_from_url(url)` has the server fetch
  an export over https from a host on `PANTAHUB_EXPORT_IMPORT_ALLOWED_HOSTS`, in
  the background; `get_export_upload` tells when it is received.

A received export's objects are stored in the account exactly as `pvr post`
stores them: each goes through `objects.CreateObject` (quota, linking to public
copies, skipped when the account has it) and its bytes through the object file
server with its size and sha256 checks. The state is kept for an hour for
`plan_revision` to merge with `import_export`. `get_export_upload` lists its
parts and signatures, and any object its state names that neither the export
nor the account has (committing such a plan would be refused).

## Security model

- **Scope of a connection.** A token is bound to this endpoint and one
  connection, carries only narrow scopes, and only reaches the devices of its
  account. The user ends a connection in the web app; resetting the password
  ends them all.
- **Nothing reaches a device unasked.** Only `commit_revision` sends anything
  to a device, only a plan, only once, only if the device has not moved on, and
  only with `trails.write`, which a client has to ask the user for separately.
- **No secrets.** No tool returns a secret, and signing keys never reach the
  Hub.
- **Links are bearer credentials.** Download and upload links work for anyone
  holding them until they expire (ten and thirty minutes), and upload links
  once. Their tokens are bound to a URL audience, so no part of the API takes
  one as a login.
- **Fetching URLs.** Imports only fetch over https, from allowed hosts,
  following redirects only to allowed hosts, and connect only to public
  addresses checked when dialling (`utils/safehttp`), so a URL cannot aim the
  server at itself or its network.
- **Bounded input.** Archives are capped in size, state documents at 32 MiB,
  plans at 20 operations, and results are capped in length.

## Tests

`go test ./mcp/` covers authentication, scopes and tool discovery. The tool
tests need a disposable MongoDB and are skipped without one; so are the export
upload tests in `exports/`:

```sh
docker run -d --rm --name mcp-test-mongo -p 127.0.0.1:27917:27017 mongo:6
PANTAHUB_MCP_TEST_MONGO=mongodb://127.0.0.1:27917 \
PANTAHUB_EXPORTS_TEST_MONGO=mongodb://127.0.0.1:27917 \
  go test ./mcp/ ./exports/ ./trails/stateops/
docker stop mcp-test-mongo
```

`trails/stateops` runs on pvr's own signed fixture and needs no database.
