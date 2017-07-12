

# Device

PANTAHUB Dash

## Start Service

Start your server:
```
./pantahub-serv
```

## Login

```
TOKEN=`http localhost:12365/auth/login username=user1 password=user1 | json token`
```

... will store access token in TOKEN for requests below

## Get Dash Content

```
http localhost:12365/dash/ Authorization:" Bearer $TOKEN"
HTTP/1.1 200 OK
Content-Length: 1239
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 15:35:08 GMT
X-Powered-By: go-json-rest

{
    "nick": "user1",
    "prn": "prn:pantahub.com:auth:/user1",
    "subscription": {
        "billing": {
            "AmountDue": 0,
            "Currency": "USD",
            "Type": "Monthly",
            "VatRegion": "World"
        },
        "plan-id": "AlphaTester",
        "quota-stats": {
            "BANDWIDTH": {
                "Actual": 0,
                "Max": 5,
                "Name": "BANDWIDTH",
                "Unit": "GiB"
            },
            "BILLINGPERIOD": {
                "Actual": 0,
                "Max": 30,
                "Name": "BILLINGPERIOD",
                "Unit": "Days"
            },
            "DEVICES": {
                "Actual": 12,
                "Max": 25,
                "Name": "DEVICES",
                "Unit": "Piece"
            },
            "OBJECTS": {
                "Actual": 0,
                "Max": 5,
                "Name": "OBJECTS",
                "Unit": "GiB"
            }
        }
    },
    "top-devices": [
        {
            "message": "Device changed at 2017-06-30 00:19:26.79 +0200 CEST",
            "nick": "honest_collie",
            "prn": "prn:::devices:/5947ca58c4a28b000e8204f4",
            "type": "INFO"
        },
        {
            "message": "Device changed at 2017-06-21 13:42:39.484 +0200 CEST",
            "nick": "polished_stallion",
            "prn": "prn:::devices:/5947c794c4a28b000e82048b",
            "type": "INFO"
        },
        {
            "message": "Device changed at 2017-06-19 19:36:56.974 +0200 CEST",
            "nick": "bursting_manatee",
            "prn": "prn:::devices:/593821ddeb775d2154a4d7c2",
            "type": "INFO"
        },
        {
            "message": "Device changed at 2017-06-19 19:36:41.096 +0200 CEST",
            "nick": "refined_walleye",
            "prn": "prn:::devices:/593d7f67eb775d2154a4f2e3",
            "type": "INFO"
        },
        {
            "message": "Device changed at 2017-06-19 19:36:19.71 +0200 CEST",
            "nick": "nice_pangolin",
            "prn": "prn:::devices:/591b6cc27a6e8e197041e83a",
            "type": "INFO"
        }
    ]
}
```


