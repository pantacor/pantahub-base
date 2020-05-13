# Callbacks

PANTAHUB Callback api's for db changes

> NOTE: Basic Authentication is required(username:saadmin,paassword:value of PANTAHUB_SA_ADMIN_SECRET in env)

> NOTE: The optional param:'timemodified'(format:RFC3339Nano or 2006-01-02T15:04:05.999999999Z07:00) is used to cancel old update callbacks

> NOTE: For the url param 'timemodified' value, please use '%2B' instead of '+' symbol for appending timezone offset while testing the api, eg: timemodified=2006-01-02T15:04:05.999%2B07:00

> NOTE: For the url param 'timemodified' value, please append 'Z' symbol to use the default db timezone, eg: timemodified=2006-01-02T15:04:05.999Z

## Callback api for device changes

### Example 1: Using default db timezone by appending 'Z' in the value of param:'timemodified'

```
http PUT localhost:12365/callbacks/devices/<ID|DEVICE_NICK>?timemodified=2006-01-02T15:04:05.999Z

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 23 Apr 2020 21:13:08 GMT
X-Powered-By: go-json-rest

{
    "device_id": 5e9ef0cefb1395295dc24173,
    "steps_marked_as_non_public": 1,
    "steps_marked_as_public": 0
}

```

### Example 2: Using a given timezone(+05:30) by using '%2B' instead of '+' symbol

```
http PUT localhost:12365/callbacks/devices/<ID|DEVICE_NICK>?timemodified=2006-01-02T15:04:05.999%2B05:30

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 23 Apr 2020 21:13:08 GMT
X-Powered-By: go-json-rest

{
    "device_id": 5e9ef0cefb1395295dc24173,
    "steps_marked_as_non_public": 1,
    "steps_marked_as_public": 0
}

```

## Callback api for step changes

### Example 1: Using the default db timezone by appending 'Z' in the value of param:'timemodified'

```
http PUT localhost:12365/callbacks/steps/<ID>?timemodified=2006-01-02T15:04:05.999Z

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 23 Apr 2020 21:13:08 GMT
X-Powered-By: go-json-rest

{
    "step_id": "5e9ef0cefb1395295dc24173-1",
    "owner": "prn:pantahub.com:auth:/user1",
    "device_id": "5e9ef0cefb1395295dc24173",
    "object_sha": [
        "A665A45920422F9D417E4867EFDC4FB8A04A1F3FFF1FA07E998E86F7F7A27AE7"
    ],
    "public": true,
    "garbage": false,
    "created_at": "2020-04-23T07:05:39.807101269+05:30",
    "updated_at": "2020-04-23T07:05:39.807101269+05:30"
}

```

### Example 2: Using a given timezone(+05:30) by using '%2B' instead of '+' symbol

```
http PUT localhost:12365/callbacks/steps/<ID>?timemodified=2006-01-02T15:04:05.999%2B05:30

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 23 Apr 2020 21:13:08 GMT
X-Powered-By: go-json-rest

{
    "step_id": "5e9ef0cefb1395295dc24173-1",
    "owner": "prn:pantahub.com:auth:/user1",
    "device_id": "5e9ef0cefb1395295dc24173",
    "object_sha": [
        "A665A45920422F9D417E4867EFDC4FB8A04A1F3FFF1FA07E998E86F7F7A27AE7"
    ],
    "public": true,
    "garbage": false,
    "created_at": "2020-04-23T07:05:39.807101269+05:30",
    "updated_at": "2020-04-23T07:05:39.807101269+05:30"
}

```
