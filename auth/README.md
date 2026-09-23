
# Auth

Using the Auth API is simple. For now we recommend using httpie command line tool
('http' below).

## Start Service

Start your server:
```
./pantahub-serv
```

## Register a user

The registration process is a two steps flow when you are using the CLI or the API, in that case in order to  
register:

1.- simply POST your details to the accounts endpoint. If nick and email are not taken 
it will response with a token and redirect-uri to continue the second step

2.- Open the browser with the redirect-uri resolve the captcha challenge and finish the process. If there is any error the system will send a registration confirm email out.

```
curl --request POST \
  --url http://localhost:12365/auth/accounts \
  --header 'content-type: application/json' \
  --data '{
	"email": "sergio.marin@pantacor.com",
	"nick": "sergiomarin",
	"password": "1234567890"
}'

{
    token: "eyJhbGciOiJSU0EtT0FFUCIsImVuYyI6IkExMjhHQ00iLCJ0eXAiOiJKV1QifQ.ExjbJW5nNxOKftqAZ0RAqMfE5Q20bzm2fcICJM-r0fqRqn3mbBadVuZrk5NqYJrCH2YfBU4Jt1yhDBjlZwM1HM4QU7Xau6IeX8h4n8kyYDd819Ra9uisYfnq2lT2mAnS-08RaFDmjh3B2o--JZDydIEAa7hXDlo7sXuIPXayj3oQVLhhpLkaTII5XYy1U47_7hPHGTf5MbXbK79yYzPS1EqtWDjLpxIiViY0oMZUgZLP_IZ81rAD_0zdTrKeh_mBUefN0d2fpcWtyH3zSGX_o6-N5-MASFc9LgVfQDBymv_9XkG_MAggv3pmfFQXeRunJZdIXJujAgFh9rEg67voOA.Eb8SnMx_c0ImvlVb.O8yTX9gqbx4a5oTGgHEMEmOBWw_aibkQs6fjgazs6OZH9u1lRXil3MSzJ4D0NlAtS-jH_Rz_hQ6gt-m-SuM_JYH-2T_-KJ67n1VugApaATIADWX-7r_d-oYkM50qcvC7U3tmiWLQ3WHveSi-1JpPPG6Ukuuw9G7itbLMpoi2AzHpMtrZPyjIv5A6JdCIFPlLax8N2krHA2lRqv24596Vv4zy_w1Y9nb3RDLX4F81F33t0VA-dE-i-Bay2RH2KH9dfm9CmlvxWpk8NwzKCgoHBHuzm7dhEVAlfNU.ucIQP10FLHcgsSNflIrO9Q",
    "redirect-uri": "http://localhost:3000/signup#account=eyJhbGciOiJSU0EtT0FFUCIsImVuYyI6IkExMjhHQ00iLCJ0eXAiOiJKV1QifQ.ExjbJW5nNxOKftqAZ0RAqMfE5Q20bzm2fcICJM-r0fqRqn3mbBadVuZrk5NqYJrCH2YfBU4Jt1yhDBjlZwM1HM4QU7Xau6IeX8h4n8kyYDd819Ra9uisYfnq2lT2mAnS-08RaFDmjh3B2o--JZDydIEAa7hXDlo7sXuIPXayj3oQVLhhpLkaTII5XYy1U47_7hPHGTf5MbXbK79yYzPS1EqtWDjLpxIiViY0oMZUgZLP_IZ81rAD_0zdTrKeh_mBUefN0d2fpcWtyH3zSGX_o6-N5-MASFc9LgVfQDBymv_9XkG_MAggv3pmfFQXeRunJZdIXJujAgFh9rEg67voOA.Eb8SnMx_c0ImvlVb.O8yTX9gqbx4a5oTGgHEMEmOBWw_aibkQs6fjgazs6OZH9u1lRXil3MSzJ4D0NlAtS-jH_Rz_hQ6gt-m-SuM_JYH-2T_-KJ67n1VugApaATIADWX-7r_d-oYkM50qcvC7U3tmiWLQ3WHveSi-1JpPPG6Ukuuw9G7itbLMpoi2AzHpMtrZPyjIv5A6JdCIFPlLax8N2krHA2lRqv24596Vv4zy_w1Y9nb3RDLX4F81F33t0VA-dE-i-Bay2RH2KH9dfm9CmlvxWpk8NwzKCgoHBHuzm7dhEVAlfNU.ucIQP10FLHcgsSNflIrO9Q"
}
```

In this case you will receive the confirmation link on the console of your pantahub server instance:

```
	To verify your account, please click on the link: <a href="http://localhost:12365/auth/verify?id=58dcb86bc094f66125a698dd&challenge=yuieui5a0ost1l2">http://localhost:12365/auth/verify?id=58dcb86bc094f66125a698dd&challenge=yuieui5a0ost1l2</a><br><br>Best Regards,<br><br>A. Sack and R. Mendoza (Pantacor Founders)
```

Simply open this url and you will be able to log in from here now.


## Authenticate

```
http POST localhost:12365/auth/login username=user1 password=user1

HTTP/1.1 200 OK
Content-Length: 256
Content-Type: application/json; charset=utf-8
Date: Fri, 19 Aug 2016 12:11:03 GMT
X-Auth-Accesstoken: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhYnJuIjoiYWJybjphYmNpZS54eXo6YXV0aDovdXNlcjEiLCJleHAiOjE0NzE2MTIyNjMsImlkIjoidXNlcjEiLCJvcmlnX2lhdCI6MTQ3MTYwODY2Mywicm9sZXMiOiJ1c2VyIiwidHlwZSI6IlVTRVIifQ.Fdwmbphn_OA7nBe9jWvWbfCbuiKcBtD0rQqEoZFBIRk

{
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhYnJuIjoiYWJybjphYmNpZS54eXo6YXV0aDovdXNlcjEiLCJleHAiOjE0NzE2MTIyNjMsImlkIjoidXNlcjEiLCJvcmlnX2lhdCI6MTQ3MTYwODY2Mywicm9sZXMiOiJ1c2VyIiwidHlwZSI6IlVTRVIifQ.Fdwmbphn_OA7nBe9jWvWbfCbuiKcBtD0rQqEoZFBIRk"
}
```

Note down the token either from json body or header for further API access.

You can use the json tool to do so automatically without hazzle:

```
TOKEN=`http POST localhost:12365/auth/login username=user1 password=user1 | json token`
```

## Account Classes

 * USER - human/botty users with id, email and secret
 * DEVICE - devices with id and secret
 * SERVICE - API services with id, location and secret

## Available users
 * user1:user1
 * user2:user2
 * device1:device1
 * device2:device2
 * service1:service1
 * service2: service2
 * service3: service3


## Refreh Token

To get a refreshed token, use the GET method with the Bearer token on the api/auth/login endpoint:

```
http GET localhost:12365/auth/login  Authorization:"Bearer $TOKEN"    
HTTP/1.1 200 OK
Content-Length: 256
Content-Type: application/json; charset=utf-8
Date: Wed, 28 Sep 2016 11:22:57 GMT
X-Auth-Accesstoken: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhYnJuIjoiYWJybjphYmNpZS54eXo6YXV0aDovdXNlcjEiLCJleHAiOjE0NzUwNjUzNzcsImlkIjoidXNlcjEiLCJvcmlnX2lhdCI6MTQ3NTA2MTI5Mywicm9sZXMiOiJ1c2VyIiwidHlwZSI6IlVTRVIifQ.R2Em_nvxzYq--EBAEXW3WKTo558PN_VwmAc4TVJ_-ek

{
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhYnJuIjoiYWJybjphYmNpZS54eXo6YXV0aDovdXNlcjEiLCJleHAiOjE0NzUwNjUzNzcsImlkIjoidXNlcjEiLCJvcmlnX2lhdCI6MTQ3NTA2MTI5Mywicm9sZXMiOiJ1c2VyIiwidHlwZSI6IlVTRVIifQ.R2Em_nvxzYq--EBAEXW3WKTo558PN_VwmAc4TVJ_-ek"
}
```

## Device registry authentication

Auth API now suppors authenticating against real device registry entries. just pass the full prn in as username
and the secret that you have put into  the device registry to do this.

Example

```
$ http POST localhost:12365/auth/login username="prn:::devices:/57ebaaddc094f6188d000002" password="yourdevicesecret"
HTTP/1.1 200 OK
Content-Length: 376
Content-Type: application/json; charset=utf-8
Date: Wed, 28 Sep 2016 16:04:07 GMT
X-Auth-Accesstoken: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhYnJuIjoiYWJybjo6OmRldmljZXM6LzU3ZWJhYWRkYzA5NGY2MTg4ZDAwMDAwMiIsImV4cCI6MTQ3NTA4MjI0NywiaWQiOiJhYnJuOjo6ZGV2aWNlczovNTdlYmFhZGRjMDk0ZjYxODhkMDAwMDAyIiwib3JpZ19pYXQiOjE0NzUwNzg2NDcsIm93bmVyIjoiYWJybjphYmNpZS54eXo6YXV0aDovdXNlcjEiLCJyb2xlcyI6ImRldmljZSIsInR5cGUiOiJERVZJQ0UifQ.7_lwQB2mk-ZvuLrNzbk1Wg_UxGe5QQp9Nr9YbhEPq8w

{
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhYnJuIjoiYWJybjo6OmRldmljZXM6LzU3ZWJhYWRkYzA5NGY2MTg4ZDAwMDAwMiIsImV4cCI6MTQ3NTA4MjI0NywiaWQiOiJhYnJuOjo6ZGV2aWNlczovNTdlYmFhZGRjMDk0ZjYxODhkMDAwMDAyIiwib3JpZ19pYXQiOjE0NzUwNzg2NDcsIm93bmVyIjoiYWJybjphYmNpZS54eXo6YXV0aDovdXNlcjEiLCJyb2xlcyI6ImRldmljZSIsInR5cGUiOiJERVZJQ0UifQ.7_lwQB2mk-ZvuLrNzbk1Wg_UxGe5QQp9Nr9YbhEPq8w"
}
```


# Service authorization with access tokens (aka oauth2'ish authorization flow)

Services are just like normal user accounts to be authenticated through the /auth/login endpoint.

Services/Clients can impersonate a user through a token exchange inspired by oauth2.

For that the service has to request from the user to issue a code with certain access scopes.

The user then uses the /auth/code endpoint to issue such accesscode and hands it over to the service/client who in turn swaps out that code for a long-lived access-token.

For example the following steps will show how the authorization flow could look like:

Step 1 - user authenticates to pantahub
```
UTOK=`http http://localhost:12365/auth/login username=user1 password=user1 | jq -r .token`
```

Step 2 - user issues authorization code for service1L
```
CODE=`http http://localhost:12365/auth/code Authorization:" Bearer $UTOK" service="prn:pantahub.com:auth:/service1" scopes="*" | jq -r .code`
```

Step 3 - service authenticates itself with pantahub
```
STOK=`http http://localhost:12365/auth/login username=service1 password=service1 | jq -r .token`
```

Step 4 - service requests swaps code for token
```
OTOK=`http http://localhost:12365/auth/token Authorization:" Bearer $STOK" access-code="$CODE" | jq -r .token`
```

Step 5. service uses access token to access pantahub on behalf of user
```
http http://localhost:12365/auth/auth_status Authorization:" Bearer $OTOK"
HTTP/1.1 200 OK
Content-Length: 243
Content-Type: application/json; charset=utf-8
Date: Wed, 20 Feb 2019 23:39:45 GMT
X-Runtime: 0.000000

{
    "aud": "prn:pantahub.com:auth:/service1",
    "id": "prn:pantahub.com:auth:/user1",
    "nick": "user1",
    "prn": "prn:pantahub.com:auth:/user1",
    "roles": "admin",
    "scopes": "*",
    "token_id": "5c6de5279c8c94c4dc06f067",
    "type": "USER"
}
```

# sudo: Admins an log in as any user

If your user has the "admin" role you get the ability to support other users.

## Get Info about all accounts

As "admin" user you can query the /accounts endpoint to retrieve account info of any user in the system:

```
http http://localhost:12365/auth/accounts?asadmin=yes Authorization:" Bearer $TOK" 

HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
Date: Mon, 18 Mar 2019 09:49:08 GMT
Transfer-Encoding: chunked
X-Runtime: 0.006500

[
    {
        "email": "asac@pantacor.com",
        "nick": "asac",
        "prn": "prn:::accounts:/58dc21d76e2bc30224f160b0"
        "time-created": "2017-03-29T23:06:31.345+02:00",
        "time-modified": "2017-03-29T23:08:56.688+02:00",
        "type": "USER"
    },
    {
...
```

## login as another user

To login as another user you can use the /auth/login endpoint by specifying the special username: "$youradminuser==>$loginasuser", e.g. 

```
http POST  localhost:12365/auth/login username='user1==>user2' password=user1

HTTP/1.1 200 OK
Content-Length: 469
Content-Type: application/json; charset=utf-8
Date: Mon, 18 Mar 2019 09:53:08 GMT
X-Runtime: 0.000183

{
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjYWxsLWFzIjp7ImlkIjoicHJuOnBhbnRhaHViLmNvbTphdXRoOi91c2VyMiIsIm5pY2siOiJ1c2VyMiIsInBybiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjIiLCJyb2xlcyI6ImFkbWluIiwidHlwZSI6IlVTRVIifSwiZXhwIjoxNTUyOTA2Mzg4LCJpZCI6InVzZXIxPT1cdTAwM2V1c2VyMiIsIm5pY2siOiJ1c2VyMSIsIm9yaWdfaWF0IjoxNTUyOTAyNzg4LCJwcm4iOiJwcm46cGFudGFodWIuY29tOmF1dGg6L3VzZXIxIiwicm9sZXMiOiJhZG1pbiIsInR5cGUiOiJVU0VSIn0.yhUwT4ExaY0KyO2_uRDlHb9kOp04lEvoL8MZ2ui3_Sk"
}

```

# Client authorization with oauth'ish approach

To mimic oauth2 implicit flow we offer /authorize endpoint that allows user to issue directly accesstoken for a preregistered client with redirect URI.

To use simply call /authorize endpoint as user:

```
OTOK=`http POST localhost:12365/auth/authorize service=prn:pantahub.com:auth:/client1 scopes='*' redirect_uri=http://localhost:808 Authorization:" Bearer $TOK" | jq -r .token`
```

And client can then use that token to call api on behalf of user given the selected scopes:

```
http POST localhost:12365/auth/authorize 
calhost:12365/devices/auth_status Authorization:" Bearer $OTOK"
HTTP/1.1 200 OK
Content-Length: 279
Content-Type: application/json; charset=utf-8
Date: Wed, 06 Mar 2019 13:38:42 GMT
X-Runtime: 0.000193

{
    "aud": "prn:pantahub.com:auth:/client1",
    "exp": "2019-03-06T15:21:28.929136745+01:00",
    "id": "prn:::accounts:/59ef9e241e7e6b000d3d2bc7",
    "nick": "asacasa",
    "prn": "prn:::accounts:/59ef9e241e7e6b000d3d2bc7",
    "roles": "admin",
    "scopes": "*",
    "token_id": "5c7fc958c094f6720b6a9bc8",
    "type": "USER"
}
```

Implicit access tokens need renewal after given expiry timeout (currently 1h)


# Standard OAuth 2.1 clients (MCP clients such as claude.ai)

The PKCE flow under `/auth/oauth` also serves clients that were never registered
here and only know the API's URL. Everything below is opt-in per request: a
client that names no `resource` and has a registered `client_id` gets exactly
what it got before, including the JSON request and response shapes.

## Discovery

`GET /.well-known/oauth-authorization-server` (RFC 8414) names the authorize and
token endpoints, the dynamic registration endpoint (when enabled), `S256` as the
only PKCE method, `none` as the only token endpoint auth method, and the scopes
of the registered resources.

## Dynamic Client Registration (RFC 7591)

Public clients without a hosted metadata document (such as OpenCode or other
CLI/desktop MCP tools) register dynamically through `POST /auth/oauth/register`,
advertised as `registration_endpoint` in the metadata document.

Registration is anonymous, so it is fenced in:

- It is only offered when a resource is registered (the MCP endpoint, with
  `PANTAHUB_MCP_ENABLED=true`) and `PANTAHUB_OAUTH_DCR_ALLOWED_REDIRECT_HOSTS`
  is set.
- Every redirect URI must be on a host in that list, comma separated, for
  example `claude.ai,127.0.0.1,localhost` (the loopback entries admit desktop
  clients). Cleartext HTTP is loopback only. An empty list turns registration
  off. It is independent of `PANTAHUB_OAUTH_CIMD_ALLOWED_HOSTS`, which only
  says where URL client ids may live.
- The client is stored as *dynamic*: its redirect URIs are matched exactly (no
  paths beneath them), it gets no scopes of its own, and it can only complete
  the authorization code flow with a `resource`, so it only ever gets
  resource-bound tokens. The polling flow (`/auth/oauth/pkce/init`), `/auth/code`
  and the implicit flow refuse it.
- Registrations are throttled per address and in total, the name and links it
  gives itself are sanitised, and a registration with no live refresh token is
  removed after 7 days (the client registers again).

## Clients identified by a URL

With `PANTAHUB_OAUTH_CIMD_ENABLED=true` a `client_id` may be an `https` URL that
serves a Client ID Metadata Document. The API fetches it, requires the document
to name that same URL as its `client_id`, and accepts only the `redirect_uris`
it lists, compared exactly (the loopback port excepted, RFC 8252). Registered
applications are PRNs, so the two kinds of client id cannot be confused.

The fetch is fenced against request forgery: `https` only, no redirects, no IP
literals, a 64 KB and 5 second budget, and a dialer that refuses any address
that is not publicly routable at connect time. Set
`PANTAHUB_OAUTH_CIMD_ALLOWED_HOSTS` (comma separated, for example `claude.ai`)
to only admit the clients you know; empty allows any public host. Fetches are
throttled per host and in total, since the authorize endpoint triggers them
without a login.

A URL client, like a dynamic one, must name a `resource`; without one the
authorize endpoint answers `invalid_target` on its redirect URI.

`GET /auth/oauth/client?client_id=<id>[&redirect_uri=<uri>]` (signed-in users
only) describes a client to the consent page. `host` is the one field the client
cannot make up: the host of a URL client id, or of the redirect URI the consent
is for (pass it). `unverified` is set for URL and dynamic clients.

## Authorization codes

The `auth_code` on the consent URL is only a handle: it has passed through the
browser of whoever started the flow. The code delivered to the redirect URI is
minted when the user approves, and the handle is never redeemable.

## Resource-bound tokens

A client that sends `resource=<url>` (RFC 8707) on the authorize request gets a
token that is only good at that resource: `aud` is the resource, `iss` is this
API, and `scopes` is what was asked for narrowed to what the resource allows,
never the account-wide scope. The REST API and the MQTT plane refuse such a
token; only the resource accepts it. Resources are registered in code
(`App.RegisterOAuthResource`); the MCP endpoint registers itself.

An unknown resource, a PKCE method other than `S256` or a `response_type` other
than `code` is reported to such a client on its redirect URI (`error=`,
`state=`, `iss=`), once that URI has been validated. Before that, errors are
shown, never redirected.

## Token endpoint

`POST /auth/oauth/token` accepts `application/x-www-form-urlencoded`, as OAuth
specifies, as well as the JSON it always took. Responses carry `access_token`
next to `token`. Form-encoded requests get RFC 6749 errors
(`{"error": "invalid_grant", "error_description": "..."}`); JSON requests keep
this API's error shape.

Resource-bound grants always get a `refresh_token`; `offline_access` is neither
needed nor advertised. `grant_type=refresh_token`
(with `client_id`) returns a new access token and a new refresh token; the one
presented is spent. Refresh tokens are stored hashed, last
`PANTAHUB_OAUTH_REFRESH_TOKEN_DAYS` (default 30) from their last use, and are
bound to their client. Presenting a spent one after a 30 second retry grace
revokes every token descended from the same consent.

## Connected applications

A resource-bound grant is a *connection*: an application that may act for the
user until they say otherwise. Every access token issued under it carries the
connection id in its `cnx` claim.

- `GET /auth/oauth/connections` lists the caller's connections: client id, the
  name the client gave itself, the host a URL client lives on (the part it
  cannot make up), the resource, the granted scopes, when it was granted, when
  it last refreshed and when it lapses if unused.
- `DELETE /auth/oauth/connections/:id` ends one. Its refresh token stops working
  and the resource stops accepting access tokens issued under it, so the
  application is cut off within seconds (the resource caches "still connected"
  for 15 seconds), not when its current token expires.

Both take a `USER` or `SESSION` login. A connected application cannot call them:
its token is resource-bound and this API refuses those. Resetting a password
ends every connection of the account.
