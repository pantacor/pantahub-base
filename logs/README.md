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
```

## Browse the Logs (USER)

As user you can navigate through your logs using the ```GET /logs/``` endpoint.

Various parameters are available to restrict and sort your search.

### Paging

You can page using the start= and page= parameters:

 * ```start``` - start offset
 * ```page``` - page size; maximum entries to return in one call

### Limit search by time

You can limit search using the "after=" and "before=" query parameter to
the ```/logs``` endpoint.

 * ```after``` - RFC3399 formatted time to limit search to log entries
   with ```time-created``` larger than this time.
 * ```before``` - RFC3399 formatted time to limit search to log entries
   with ```time-created``` smaller than this time.

At this point behavioru if both parameters are found in query is undefined.

### Streaming

To realize streaming typically you would query for logs and then use the date
of last item retrieved as after= parameter until you retrieve a new item.

Also see below for Cursor feature which gives you a good way to step through
long lists sorted by keys that are not unique.

### Cursors

If you want to scroll through later lists you can use cursor feature. With cursor
you can use the ```/logs/cursor``` to page throught the search results whose
initial invocation of the ```/logs``` endpoint got passed ```cursor=true``` a query
parameter.

Until the cursor gets exhausted the result page of ```/logs``` and ```/logs/cursor```
endpoints will return a ```next-cursor``` field that contains the cursor you will
have to pass to ```/logs/cursor``` to retrieve the next page.

Note that cursors do get invalidated if they get exhausted (meaning: you retrieved
the last entries). In case the cursor is found to be exhausted by ```/logs``` or
```/logs/cursor``` endpoint, ```next-cursor``` will be empty string ("").`  `


### Examples

#### Example: Get log

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

### Example: limit search with ```after```:

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

#### Example: sorting
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


#### Example: logs with cursor

```
http GET localhost:12365/logs/?cursor=true Authorization:" Bearer $TOK"
HTTP/1.1 200 OK
Connection: keep-alive
Content-Encoding: gzip
Content-Type: application/json; charset=utf-8
Date: Mon, 15 Oct 2018 09:10:50 GMT
Server: nginx/1.13.5
Strict-Transport-Security: max-age=15724800; includeSubDomains;
Transfer-Encoding: chunked
X-Powered-By: go-json-rest
X-Runtime: 0.719266

{
    "count": 50,
    "entries": [
        {
            "dev": "prn:::devices:/5b27dbfbadf5440009c5020b",
            "id": "5bbd2bb56629c6000954cb8c",
            "lvl": "DEBUG",
            "msg": "going to state = STATE_WAIT(617)",
            "own": "prn:::accounts:/59ef9e241e7e6b000d3d2bc7",
            "src": "controller",
            "time-created": "2018-10-09T22:29:09.430488437Z",
            "tnano": 62937,
            "tsec": 1539124149
        },

[...]

    ],
    "next-cursor": "eyJhbGciOiJIUzI1N.....XU5xwIrtI4M",
    "page": 50,
    "start": 0
}

```

And use the value of ```next-cursor``` for follow up calls to the cursor endpoint:

```
http  POST localhost:12365/logs/cursor next-cursor="$next" Authorization:" Bearer $TOK"
...
```

This will also include a fresh ```next-cursor```, please use that for doing the next call.

