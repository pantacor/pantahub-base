# Cron jobs

PANTAHUB Cron job api's

NOTE: Basic Authentication is required(username:saadmin,paassword:value of PANTAHUB_SA_ADMIN_SECRET in env)

## Cron job api for processing public devices

```
http PUT localhost:12365/cron/public/devices

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 23 Apr 2020 21:13:08 GMT
X-Powered-By: go-json-rest

[
    {
        "device_id": "5e9ef0cefb1395295dc24173",
        "steps_marked_as_non_public": 0,
        "steps_marked_as_public": 2
    },
    {
        "device_id": "5e9ef0cefb1395295dc24176",
        "steps_marked_as_non_public": 0,
        "steps_marked_as_public": 6
    }
]
```

## Cron job api for processing public steps

```
http PUT localhost:12365/cron/steps

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 23 Apr 2020 21:13:08 GMT
X-Powered-By: go-json-rest

[
    {
        "step_id": "5e9ef0cefb1395295dc24173-0",
        "owner": "prn:pantahub.com:auth:/user1",
        "device_id": "5e9ef0cefb1395295dc24173",
        "object_sha": [],
        "public": true,
        "garbage": false,
        "created_at": "2020-04-23T00:24:52.698Z",
        "updated_at": "2020-04-23T08:20:10.668259559+05:30"
    },
    {
        "step_id": "5e9ef0cefb1395295dc24173-1",
        "owner": "prn:pantahub.com:auth:/user1",
        "device_id": "5e9ef0cefb1395295dc24173",
        "object_sha": [
            "A665A45920422F9D417E4867EFDC4FB8A04A1F3FFF1FA07E998E86F7F7A27AE7"
        ],
        "public": true,
        "garbage": false,
        "created_at": "2020-04-23T01:33:53.606Z",
        "updated_at": "2020-04-23T08:20:10.670712466+05:30"
    }
]

```
