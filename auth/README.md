
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
X-Powered-By: go-json-rest

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
X-Powered-By: go-json-rest

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
X-Powered-By: go-json-rest

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
X-Powered-By: go-json-rest
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
X-Powered-By: go-json-rest
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
X-Powered-By: go-json-rest
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
X-Powered-By: go-json-rest
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

