

# Device

PANTAHUB Device Registry

NOTE: the auth service does not take the secrets here into account yet!

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

## Upload File

### Register a Device (As User)

```
http POST localhost:12365/devices/  Authorization:"Bearer $TOKEN" \
    secret="yourdevicesecret"

HTTP/1.1 200 OK
Content-Length: 331
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:13:08 GMT
X-Powered-By: go-json-rest

{
    "challenge": "likely-creative-troll",
    "device-meta": {},
    "id": "596690e4632d7234e270360f",
    "nick": "proper_sponge",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/596690e4632d7234e270360f",
    "secret": "yourdevicesecret",
    "time-created": "2017-07-12T23:13:08.199223323+02:00",
    "time-modified": "0001-01-01T00:00:00Z",
    "user-meta": {}
}
```

### Register a Device for Claiming (As Device)

To allow distribution of images and to allow transfer of ownership, devices
that have no owner can have a challenge field that allows an authenticated
user to claim it through its PUT endpoint.

In example of generic image a device would first register itself with its own
secret to the hub and would present the user with a challenge that he can
use to claim it.

Example:

1. device registers itself
```
http POST localhost:12365/devices/ secret="mysec1"
HTTP/1.1 200 OK
Content-Length: 286
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:14:34 GMT
X-Powered-By: go-json-rest

{
    "challenge": "duly-helped-bat",
    "device-meta": {},
    "id": "59669139632d7234e2703610",
    "nick": "immortal_worm",
    "owner": "",
    "prn": "prn:::devices:/59669139632d7234e2703610",
    "secret": "mysec1",
    "time-created": "2017-07-12T23:14:33.97805159+02:00",
    "time-modified": "0001-01-01T00:00:00Z",
    "user-meta": {}
}


Note how the output has a challenge entry, but no owner yet...

2. extract the challenge from the json and display to user
```
challenge=duly-helped-bat
```
3. as a logged in user with TOKEN you claim the device through a simple PUT
```
http PUT localhost:12365/devices/59669139632d7234e2703610?challenge=$challenge Authorization:"Bearer $TOKEN"
HTTP/1.1 200 OK
Content-Length: 309
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:15:49 GMT
X-Powered-By: go-json-rest

{
    "challenge": "",
    "device-meta": {},
    "id": "59669139632d7234e2703610",
    "nick": "immortal_worm",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/59669139632d7234e2703610",
    "secret": "mysec1",
    "time-created": "2017-07-12T23:14:33.978+02:00",
    "time-modified": "2017-07-12T23:15:49.078077992+02:00",
    "user-meta": {}
}
```

As you can see the challenge field is now reset and the owner is assigned.


### Get Your Devices

... only yours!

```
http localhost:12365/devices/  Authorization:"Bearer $TOKEN"
HTTP/1.1 200 OK
Content-Length: 345
Content-Type: application/json; charset=utf-8
Date: Tue, 04 Oct 2016 20:43:25 GMT
X-Powered-By: go-json-rest

[
    {
        "prn": "prn:::devices:/57f41438b376a825cf000001", 
        "id": "57f41438b376a825cf000001", 
        "nick": "desired_stud", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "secret": "yourdevicesecret"
    }, 
    {
        "prn": "prn:::devices:/57f4146bb376a825cf000002", 
        "id": "57f4146bb376a825cf000002", 
        "nick": "composed_pheasant", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "secret": "anotherdevice"
    }
]

## select your device here from above list
DEVICEID=57f41438b376a825cf000001
```

### Change Device Secret

```
http PUT localhost:12365/devices/$DEVICEID  Authorization:"Bearer $TOKEN" \
	secret="mynewsecret1"

HTTP/1.1 200 OK
Content-Length: 324
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:16:58 GMT
X-Powered-By: go-json-rest

{
    "challenge": "",
    "device-meta": {},
    "id": "59669139632d7234e2703610",
    "nick": "immortal_worm",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/59669139632d7234e2703610",
    "secret": "mynewsecret1",
    "time-created": "2017-07-12T23:14:33.978+02:00",
    "time-modified": "2017-07-12T23:16:58.707242972+02:00",
    "user-meta": {}
}

```

# Fill in User Metadata

To fill in user metadata you need to be logged in as user like
for all operations above:

```
http PUT localhost:12365/devices/$DEVICEID/user-meta Authorization:" Bearer $TOKEN" \
	some=user meta=datafields
HTTP/1.1 200 OK
Content-Length: 35
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:21:18 GMT
X-Powered-By: go-json-rest

{
    "meta": "datafields",
    "some": "user"
}
```

Afterwards your device will have this metadata filled in:
```
http GET localhost:12365/devices/$DEVICEID Authorization:" Bearer $TOKEN" 
HTTP/1.1 200 OK
Content-Length: 351
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:21:47 GMT
X-Powered-By: go-json-rest

{
    "challenge": "",
    "device-meta": {},
    "id": "59669139632d7234e2703610",
    "nick": "immortal_worm",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/59669139632d7234e2703610",
    "secret": "mynewsecret1",
    "time-created": "2017-07-12T23:14:33.978+02:00",
    "time-modified": "2017-07-12T23:16:58.707+02:00",
    "user-meta": {
        "meta": "datafields",
        "some": "user"
    }
}
```

# Fill in Device Metadata

To fill in user metadata you need to be logged in as the device:

```
DTOKEN=`http localhost:12365/auth/login username=prn:::devices:/$DEVICEID password=mynewsecret1 | json token`
```

After that you can update device metadata in same way as the user meta data above using
the device credentials:

```
http PUT localhost:12365/devices/$DEVICEID/device-meta Authorization:" Bearer $DTOKEN" some=device meta=datafields work=too
HTTP/1.1 200 OK
Content-Length: 50
Content-Type: application/json; charset=utf-8
Date: Wed, 12 Jul 2017 21:26:49 GMT
X-Powered-By: go-json-rest

{
    "meta": "datafields",
    "some": "device",
    "work": "too"
}

```

# Share your device with the world; mark it as public

To mark device as public you can either update the public field
using a full PUT on the device you you can use the convenience
endpoint ```/devices/:id/public```:

```
http PUT localhost:12365/devices/$DEVICEID/public Authorization:" Bearer $TOKEN"
HTTP/1.1 200 OK
Content-Length: 329
Content-Type: application/json; charset=utf-8
Date: Wed, 18 Oct 2017 20:09:55 GMT
X-Powered-By: go-json-rest

{
...
    "public": true,
...
}
```

# Unshare your devices; unmark them public

If you change your mind simply use the DELETE method on the public endpoint:

```
http PUT localhost:12365/devices/$DEVICEID/public Authorization:" Bearer $TOKEN"
HTTP/1.1 200 OK
Content-Length: 330
Content-Type: application/json; charset=utf-8
Date: Wed, 18 Oct 2017 20:09:52 GMT
X-Powered-By: go-json-rest

{
...
    "public": false,
...
}
```

