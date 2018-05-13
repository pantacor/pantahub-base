# Logs

PANTAHUB Logs Service

## Start Service

Logs is part of pantahub-base. Start your base server:
```
./pantahub-base
```

## Login

Login as a user:

```
TOKEN=`http localhost:12365/auth/login username=user1 password=user1 | json token`
```
... will store access token in TOKEN for USER requests below


Login as a device:

```
DTOKEN=`http localhost:12365/auth/login username=device1 password=device1| json token`
```
... will store access token in TOKEN for DEVICE requests below


## Post Log Entries (DEVICE)

Devices post log entries by POSTING elements to the logs endpoint.

Mandatory fields are:
 * src - what log is this from?
 * msg - what is the message?

Recommended fields are:
 * tsec - time in seconds since 1970
 * tnano - nanoseconds in seconds
 * lvl - severity/log/debug level

Implicit fields are:
 * dev - device id; will be extracted from login context for Devices
 * time-created - time when this entry first became known to the logs endpoint


You can post either a single entry or a json error for batch submission:

### Write single entry (DEVICE)

Option 1 (post single entry)
```
http POST localhost:12365/logs/ Authorization:" Bearer $DTOKEN" \
                                src="pantavisor.log" \
                                msg="My log line to remember" \
							   lvl="INFO" \
							   tsec="1496532292" \
							   tnano="802110514"
```

### Write batch of entries (DEVICE)

Option 2 (post a batch)
```
http POST localhost:12365/logs/ Authorization:" Bearer $DTOKEN" <<EOF
[
 { "src": "pantavisor.log",
   "msg": "message 1 text",
   "lvl": "INFO",
   "tsec": 1496532292,
   "tnano": 802110514
 },
 { "src": "pantavisor.log",
   "msg": "message 2 text",
   "lvl": "INFO",
   "tsec": 1496532322,
   "tnano": 802110545
 }
]
EOF

## Browse the Logs (USER)

As user you can navigate through your logs using the GET endpoint.

Various parameters are available to restrict and sort your search.

Paging:
You can page using the start= and page= parameters. Start has to be a number
a number used as offset for paging.

Streaming:
You can implement poll streaming by using the "after" parameter which takes
an RFC3399 formatted date/time as input and ensures that results will be
considered only for new entries that were added to db after that date.
Typically you would query for logs and then use the date of last item
retrieved as after= parameter until you retrieve a new item.


Example: Get log

```
http GET localhost:12365/logs/ Authorization:" Bearer $TOKEN" \
			start=0 \
			page=50

HTTP/1.1 200 OK
Content-Length: 1999
Content-Type: application/json; charset=utf-8
Date: Sat, 03 Jun 2017 23:44:46 GMT
X-Powered-By: go-json-rest

{
    "count": 8,
    "entries": [
        {
            "dev": "prn:pantahub.com:auth:/device1",
            "id": "59309891632d7256597b03d2",
            "lvl": "INFO",
            "msg": "MyMessage 4 single",
            "own": "prn:pantahub.com:auth:/user1",
            "src": "pantavisor.log",
            "time-created": "2017-06-02T00:43:29.136+02:00",
            "tnano": 121312212,
            "tsec": 123213
        },

	[...]

        {
            "dev": "prn:pantahub.com:auth:/device1",
            "id": "5930943d632d724db7c123e4",
            "lvl": "INFO",
            "msg": "MyMessage",
            "own": "prn:pantahub.com:auth:/user1",
            "src": "pantavisor.log",
            "time-created": "2017-06-02T00:25:01.885+02:00",
            "tnano": 12312212,
            "tsec": 123113
        }
    ],
    "page": 50,
    "start": 0
}

```

Example (with after):

```
http GET "localhost:12365/logs/?after=2017-06-02T00:25:01.885%2B02:00 "Authorization:" Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 1999
Content-Type: application/json; charset=utf-8
Date: Sat, 03 Jun 2017 23:44:46 GMT
X-Powered-By: go-json-rest

{
    "count": 1,
    "entries": [
        {
            "dev": "prn:pantahub.com:auth:/device1",
            "id": "59309891632d7256597b03d2",
            "lvl": "INFO",
            "msg": "MyMessage 4 single",
            "own": "prn:pantahub.com:auth:/user1",
            "src": "pantavisor.log",
            "time-created": "2017-06-02T00:43:29.136+02:00",
            "tnano": 121312212,
            "tsec": 123213
        }

    ],
    "page": 50,
    "start": 0
}

```


Sorting:
You can sort the logs by time-created.

```
http GET 'localhost:12365/logs/?src=pantavisor.log&sort=-time-created' \
				Authorization:" Bearer $TOKEN"

{
    "count": 8,
    "entries": [
        {
            "dev": "prn:pantahub.com:auth:/device1",
            "id": "5930943d632d724db7c123e4",
            "lvl": "INFO",
            "msg": "MyMessage",
            "own": "prn:pantahub.com:auth:/user1",
            "src": "pantavisor.log",
            "time-created": "2017-06-02T00:25:01.885+02:00",
            "tnano": 12312212,
            "tsec": 123113
        },

	[...]

        {
            "dev": "prn:pantahub.com:auth:/device1",
            "id": "59309891632d7256597b03d2",
            "lvl": "INFO",
            "msg": "MyMessage 4 single",
            "own": "prn:pantahub.com:auth:/user1",
            "src": "pantavisor.log",
            "time-created": "2017-06-02T00:43:29.136+02:00",
            "tnano": 121312212,
            "tsec": 123213
        }
    ],
    "page": 50,
    "start": 0
}

```

All fields available for sorting are:
 * tsec,tnano,device,src,lvl,time-created



