# Profile

PANTAHUB User profile

NOTE: Profiles are implicitly created for all accounts

```bash
curl --request PUT \
  --url http://localhost:12365/apps/ \
  --header 'authorization: Bearer $TOKEN' \
  --header 'content-type: application/json' \
  --data '{
  "fullName": "Sergio Marin",
  "bio": "Pantacor developer",
  "location": "Santiago, Chile",
  "picture": "base64 encode Image 200x200px",
  "website": "https://highercomve.github.io",
  "github": "https://github.com/highercomve",
  "gitlab": "https://gitlab.com/highercomve",
  "company": "Pantacor",
  "twitter": "https://twitter.com/pantahub",
  "time-created": "2020-10-08T15:29:27.045Z",
  "time-modified": "2020-10-08T15:30:23.489Z"
}'
```

## Get All profiles

```
http GET localhost:12365/profiles  Authorization:"Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:13:08 GMT
X-Powered-By: go-json-rest

[
    {
        "nick": "efg",
        "bio": "",
        "public": false,
        "garbage": false,
        "time-created": "0001-01-01T00:00:00Z",
        "time-modified": "0001-01-01T00:00:00Z"
    },
    {
        "nick": "abc",
        "bio": "",
        "public": false,
        "garbage": false,
        "time-created": "0001-01-01T00:00:00Z",
        "time-modified": "0001-01-01T00:00:00Z"
    }
]

```

## Get All profiles having nick starts with abc

```
http GET localhost:12365/profiles?nick=^abc  Authorization:"Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:13:08 GMT
X-Powered-By: go-json-rest

[
    {
        "nick": "abc",
        "bio": "",
        "public": false,
        "garbage": false,
        "time-created": "0001-01-01T00:00:00Z",
        "time-modified": "0001-01-01T00:00:00Z"
    }
]

```

## Get a profiles by user ID

```
http GET localhost:12365/profiles/pantahub-ci  Authorization:"Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:13:08 GMT
X-Powered-By: go-json-rest

 {
        "nick": "abc",
        "bio": "",
        "public": false,
        "garbage": false,
        "time-created": "0001-01-01T00:00:00Z",
        "time-modified": "0001-01-01T00:00:00Z"
 }

```

## Using Pagination(By default page=0 & limit=20)

```
http GET localhost:12365/profiles/?page=3&limit=2  Authorization:"Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:13:08 GMT
X-Powered-By: go-json-rest

[
    {
        "nick": "efg",
        "bio": "",
        "public": false,
        "garbage": false,
        "time-created": "0001-01-01T00:00:00Z",
        "time-modified": "0001-01-01T00:00:00Z"
    },
    {
        "nick": "abc",
        "bio": "",
        "public": false,
        "garbage": false,
        "time-created": "0001-01-01T00:00:00Z",
        "time-modified": "0001-01-01T00:00:00Z"
    }
]

```
