
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
```

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

## Fill in User Metadata

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

## Fill in Device Metadata

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

## Share your device with the world; mark it as public

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

## Unshare your devices; unmark them public

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

## Change device nick

To change device nick, use the PATCH method on the device resource, e.g.

```
http PATCH localhost:12365/devices/$DEVICEID/ nick=mynewnick Authorization:" Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 273
Content-Type: application/json; charset=utf-8
Date: Wed, 23 May 2018 20:43:10 GMT
X-Powered-By: go-json-rest
X-Runtime: 0.000977

{
    "device-meta": {},
    "id": "5aadaf259c8c9433706048ab",
    "nick": "mynewnick",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/XXXXXXXXXXXXXXXXXXXXXXXXXXX",
    "public": false,
    "time-created": "2018-03-18T01:13:25.11+01:00",
    "time-modified": "0001-01-01T00:00:00Z",
    "user-meta": {}
}
```

If nick is already taken, clients will get a 409 (Conflict) http status code:

```
http PATCH http://localhost:12365/devices/$DEVICE_ID Authorization:" Bearer $TOK" nick=already-taken
HTTP/1.1 409 Conflict
Content-Length: 45
Content-Type: application/json; charset=utf-8
Date: Wed, 23 May 2018 20:40:07 GMT
X-Powered-By: go-json-rest
X-Runtime: 0.001955

{
    "Error": "Device unique constraint violated"
}
```

## Auto Assign Devices to Owners

Pantahub base offers a built in basic factory story in the sense that we offer the ability to auto assing devices to a specific owner.

For that right now we use a simple token based approach:

 1. Owner uses ```/devices/tokens/``` end point to create a new token; optionally he can also provide a set of default user-meta information that the auto assign feature will put in place for every device joinig using such token.
 2. Token is a one-time-visible secret that will only be displayed on reply of the token registration, but not afterwards. If user looses a token he can generate a new one. Old token can stay active if user does not believe the token has been compromised
 3. User configures device at factory to use the produced token as its pantahub registration credential. Pantavisor will then use the token when registering itself for first time. It uses ```Pantahub-Devices-Auto-Token-V1``` to pass the token to pantahub when registering itself. With this pantahub will auto assign the device to the owner of the given token and will put UserMeta in place.

Example:

```
http --print=bBhH POST localhost:12365/devices/tokens Authorization:" Bearer $TOK" default-user-meta:='{"mykey": "yourvalue"}'
POST /devices/tokens HTTP/1.1
Accept: application/json, */*
Accept-Encoding: gzip, deflate
Authorization: Bearer eyJxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx...
Connection: keep-alive
Content-Length: 45
Content-Type: application/json
Host: localhost:12365
User-Agent: HTTPie/0.9.9

{
    "default-user-meta": {
        "mykey": "yourvalue"
    }
}

HTTP/1.1 200 OK
Content-Length: 370
Content-Type: application/json; charset=utf-8
Date: Mon, 10 Dec 2018 15:46:39 GMT
X-Powered-By: go-json-rest
X-Runtime: 0.000635

{
    "default-user-meta": {
        "mykey": "yourvalue"
    }, 
    "disabled": false, 
    "id": "5c0e8a5fc094f62eafcc96b4", 
    "nick": "informally_trusted_dingo", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "prn": "prn:::devices-tokens:/5c0e8a5fc094f62eafcc96b4", 
    "time-created": "2018-12-10T16:46:39.740026537+01:00", 
    "time-modified": "2018-12-10T16:46:39.740026537+01:00", 
    "token": "xxxxxxxxxxxx"
}
```
**Remember the ```token```* you won't be able to retrieve it another time.

### Auto Assign Devices on Registration

Now on device side you have to send the Pantahub-Devices-Auto-Token-V1: http header when registering yourself. This will make pantahub to automatically associated you with the owner of the token. Example:

```
http --print=bBhH POST localhost:12365/devices/ Pantahub-Devices-Auto-Token-V1:Fn5xxxxxxxxxxxxxxxxxxxxxxxxxxx
POST /devices/ HTTP/1.1
Accept: */*
Accept-Encoding: gzip, deflate
Connection: keep-alive
Content-Length: 0
Host: localhost:12365
Pantahub-Devices-Auto-Token-V1: Fn5xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
User-Agent: HTTPie/0.9.9



HTTP/1.1 200 OK
Content-Length: 353
Content-Type: application/json; charset=utf-8
Date: Mon, 10 Dec 2018 16:03:35 GMT
X-Powered-By: go-json-rest
X-Runtime: 0.000788

{
    "device-meta": {},
    "id": "5c0e8e57c094f62eafcc96b8",
    "nick": "privately_correct_quail",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/5c0e8e57c094f62eafcc96b8",
    "public": false,
    "secret": "0jf4yyyyyyyyyyyy",
    "time-created": "2018-12-10T17:03:35.489788421+01:00",
    "time-modified": "2018-12-10T17:03:35.489788421+01:00",
    "user-meta": {
        "mykey": "yourvalue"
    }
}
```


### List tokens registered

If you want to see which tokens are already registered by your user, you can do so through a simple get at collection level:

```
http --print=bBhH GET localhost:12365/devices/tokens Authorization:" Bearer $TOK"
GET /devices/tokens HTTP/1.1
Accept: */*
Accept-Encoding: gzip, deflate
Authorization: Bearer xxxxxxxxxxxxxxXxxxxxxxxxxxxxxxxxxxxxxxxxxXXXXXxxxxxXXXXXXXXXXXXXXxxxxXXXXxxxxxxxxQ
Connection: keep-alive
Host: localhost:12365
User-Agent: HTTPie/0.9.9



HTTP/1.1 200 OK
Content-Length: 1256
Content-Type: application/json; charset=utf-8
Date: Mon, 10 Dec 2018 15:48:14 GMT
X-Powered-By: go-json-rest
X-Runtime: 0.000664

[
    {
        "default-user-meta": {
            "mykey": "yourvalue"
        }, 
        "disabled": false, 
        "id": "5c0e8a5fc094f62eafcc96b4", 
        "nick": "informally_trusted_dingo", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "prn": "prn:::devices-tokens:/5c0e8a5fc094f62eafcc96b4", 
        "time-created": "2018-12-10T16:46:39.74+01:00", 
        "time-modified": "2018-12-10T16:46:39.74+01:00"
    }, 
    {
        "default-user-meta": {
            "mykey": "yourvalue"
        }, 
        "disabled": true, 
        "id": "5c0e6b93c094f67d861e9444", 
        "nick": "entirely_fit_badger", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "prn": "prn:::devices-tokens:/5c0e6b93c094f67d861e9444", 
        "time-created": "2018-12-10T14:35:15.984+01:00", 
        "time-modified": "2018-12-10T14:35:15.984+01:00"
    }, <>
    {
        "default-user-meta": {
            "mykey": "yourvalue"
        }, 
        "disabled": true, 
        "id": "5c0e5affc094f6050521ae9c", 
        "nick": "correctly_literate_camel", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "prn": "prn:::devices-tokens:/5c0e5affc094f6050521ae9c", 
        "time-created": "2018-12-10T13:24:31.615+01:00", 
        "time-modified": "2018-12-10T13:24:31.615+01:00"
    }, 
    {
        "default-user-meta": {
            "mykey": "yourvalue"
        }, 
        "disabled": true, 
        "id": "5c0e5ad6c094f603c3d11f7c", 
        "nick": "miserably_singular_insect", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "prn": "prn:::devices-tokens:/5c0e5ad6c094f603c3d11f7c", 
        "time-created": "2018-12-10T13:23:50.359+01:00", 
        "time-modified": "2018-12-10T13:23:50.359+01:00"
    }
]
```


### Disable Tokens

In case you want to ensure no further devices will be auto assinged you can disable your token in pantahub. Existing devices will continue to be authenticated though.

```
http --print=bBhH  DELETE localhost:12365/devices/tokens/5c0e8a5fc094f62eafcc96b4 Authorization:" Bearer $TOK"
DELETE /devices/tokens/5c0e8a5fc094f62eafcc96b4 HTTP/1.1
Accept: */*
Accept-Encoding: gzip, deflate
Authorization: Bearer XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXx
Connection: keep-alive
Content-Length: 0
Host: localhost:12365
User-Agent: HTTPie/0.9.9



HTTP/1.1 200 OK
Content-Length: 15
Content-Type: application/json; charset=utf-8
Date: Mon, 10 Dec 2018 15:52:19 GMT
X-Powered-By: go-json-rest
X-Runtime: 0.000664

{
    "status": "OK"
}
...
```
