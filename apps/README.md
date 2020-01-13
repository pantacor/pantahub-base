Third Party Application Service
====================

This service handle the creation, update, read and delete of third party applications. This applications can use pantahub oauth to authenticate their users and ask permission to different scopes inside pantahub base and can create their own scopes.

For most of the endpoints you will need a TOKEN to identify the owner of the application

#### Login

```
TOKEN=`http localhost:12365/auth/login username=user1 password=user1 | json token`
```

## Retrive Pantahub avaliable scopes (Public endpoint)

```bash
curl --request GET \
  --url http://localhost:12365/apps/scopes \
  --header 'content-type: application/json'
```

**Response:**

```json
[
  {
    "id": "all",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Complete Access"
  },
  {
    "id": "user.readonly",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read only user"
  },
  {
    "id": "user.write",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Write only user"
  },
  {
    "id": "devices",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read/Write devices"
  },
  {
    "id": "devices.readonly",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read only devices"
  },
  {
    "id": "devices.write",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Write only devices"
  },
  {
    "id": "devices.change",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Update devices"
  },
  {
    "id": "objects",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read/Write only objects"
  },
  {
    "id": "objects.readonly",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read only objects"
  },
  {
    "id": "objects.write",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Write only objects"
  },
  {
    "id": "objects.change",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Update objects"
  },
  {
    "id": "trails",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read/Write only trails"
  },
  {
    "id": "trails.readonly",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read only trails"
  },
  {
    "id": "trails.write",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Write only trails"
  },
  {
    "id": "trails.change",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Update trails"
  },
  {
    "id": "metrics",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read/Write only metrics"
  },
  {
    "id": "metrics.readonly",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Read only metrics"
  },
  {
    "id": "metrics.write",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Write only metrics"
  },
  {
    "id": "metrics.change",
    "service": "prn:pantahub.com:apis:/base",
    "description": "Update metrics"
  }
]
```

## Create App

In order to create an app you need to send a json with this 3 obligatory properties:

- type: One application can have two types (public|condidential) (more about that in here)[https://tools.ietf.org/html/rfc6749#section-2.1].
- redirect_uris: This is an array of string with the URLs where can redirect the oauth service to sent the token or code.
- scopes: this is an array of scopes, this set a approved list of scopes that can be asked to the user to give permission. 

```bash
curl --request POST \
  --url http://localhost:12365/apps/ \
  --header 'authorization: Bearer $TOKEN' \
  --header 'content-type: application/json' \
  --data '{
	"type": "public",
	"redirect_uris": ["http://localhost/return_url"],
	"scopes": [
		{
			"id": "all",
			"service": "prn:pantahub.com:apis:/base"
		}
	]
}'
```

**Response:**

```json
{
  "id": "5e0a658db0acd7109320fbe0",
  "type": "public",
  "nick": "secretly_better_grouper",
  "prn": "prn:pantahub.com:apis:/5e0a658db0acd7109320fbe0",
  "owner": "prn:::accounts:/5dfaac1b883859b4de940ca9",
  "owner-nick": "highercomve",
  "secret": "ct6bzdrzhaya7ezc75wy2ocuw6qz1v",
  "redirect_uris": [
    "http://localhost/return_url"
  ],
  "scopes": [
    {
      "id": "all",
      "service": "prn:pantahub.com:apis:/base",
      "description": "Complete Access"
    }
  ],
  "time-created": "2019-12-30T21:01:01.253338883Z",
  "time-modified": "2019-12-30T21:01:01.253338883Z"
}
```

## Get all apps of a user 

```bash
curl --request GET \
  --url http://localhost:12365/apps/ \
  --header 'authorization: Bearer $TOKEN' \
  --header 'content-type: application/json'
```

## Get app by ID

```bash
curl --request GET \
  --url http://localhost:12365/apps/5e0a658db0acd7109320fbe0 \
  --header 'authorization: Bearer $TOKEN' \
  --header 'content-type: application/json'
```

## Update app

```bash
curl --request PUT \
  --url http://localhost:12365/apps/5e0a658db0acd7109320fbe0 \
  --header 'authorization: Bearer $TOKEN' \
  --header 'content-type: application/json' \
  --data '{
	"type": "public",
	"redirect_uris": [
		"http://posibleappurl.com/oauth2/cb",
		"https://posibleappurl.com/oauth2/cb"
	],
	"scopes": [
		{
			"id": "all",
			"service": "prn:pantahub.com:apis:/base",
			"description": "Complete Access"
		},
		{
			"id": "programs.all",
			"description": "Read/write programs from the thirdparty application"
		}
	] 
}'
```

## Delete APP

```bash
curl --request DELETE \
  --url http://localhost:12365/apps/5e0a658db0acd7109320fbe0 \
  --header 'authorization: Bearer $TOKEN' \
  --header 'content-type: application/json'
```