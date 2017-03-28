

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
Content-Length: 170
Content-Type: application/json; charset=utf-8
Date: Tue, 04 Oct 2016 20:42:32 GMT
X-Powered-By: go-json-rest

{
    "prn": "prn:::devices:/57f41438b376a825cf000001", 
    "id": "57f41438b376a825cf000001", 
    "nick": "desired_stud", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "secret": "yourdevicesecret"
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
http POST localhost:12365/devices secret="mysec1"
{
  "id": "58b0bbf0c094f605418b1a84",
  "prn": "prn:::devices:/58b0bbf0c094f605418b1a84",
  "nick": "growing_dodo",
  "owner": "",
  "secret": "mysec1",
  "time-created": "2017-02-25T00:04:16.568431302+01:00",
  "time-modified": "0001-01-01T00:00:00Z",
  "challenge": "probably-relieved-insect"
}

Note how the output has a challenge entry, but no owner yet...

2. extract the challenge from the json and display to user
```
challenge=probably-relieved-insect
```
3. as a logged in user with TOKEN you claim the device through a simple PUT
```
http PUT localhost:12365/devices/58b0bbf0c094f605418b1a84?challenge=$challenge Authorization:"Bearer $TOKEN"
HTTP/1.1 200 OK
Content-Length: 276
Content-Type: application/json; charset=utf-8
Date: Fri, 24 Feb 2017 23:11:02 GMT
X-Powered-By: go-json-rest

{
    "challenge": "",
    "id": "58b0bbf0c094f605418b1a84",
    "nick": "growing_dodo",
    "owner": "prn:pantahub.com:auth:/user1",
    "prn": "prn:::devices:/58b0bbf0c094f605418b1a84",
    "secret": "mysec1",
    "time-created": "2017-02-25T00:04:16.568+01:00",
    "time-modified": "2017-02-25T00:11:02.251026642+01:00"
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
    secret="mynewdevicesecret"


HTTP/1.1 200 OK
Content-Length: 171
Content-Type: application/json; charset=utf-8
Date: Tue, 04 Oct 2016 20:44:29 GMT
X-Powered-By: go-json-rest

{
    "prn": "prn:::devices:/57f41438b376a825cf000001", 
    "id": "57f41438b376a825cf000001", 
    "nick": "desired_stud", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "secret": "mynewdevicesecret"
}
```


