
# Auth

Using the Auth API is simple. For now we recommend using httpie command line tool
('http' below).

## Start Service

Start your server:
```
./pantahub-serv
```

## Authenticate

```
http POST localhost:12365/api/auth/login username=user1 password=user1

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
TOKEN=`http POST localhost:12365/api/auth/login username=user1 password=user1 | json token`
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
http GET localhost:12365/api/auth/login  Authorization:"Bearer $TOKEN"    
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
$ http POST localhost:12365/api/auth/login username="prn:::devices:/57ebaaddc094f6188d000002" password="yourdevicesecret"
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


