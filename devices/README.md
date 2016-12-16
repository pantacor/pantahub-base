

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
TOKEN=`http localhost:12365/api/auth/login username=user1 password=user1 | json token`
```

... will store access token in TOKEN for requests below

## Upload File

### Register a Device

```
http POST localhost:12365/api/devices/  Authorization:"Bearer $TOKEN" \
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

### Get Your Devices

... only yours!

```
http localhost:12365/api/devices/  Authorization:"Bearer $TOKEN"
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
http PUT localhost:12365/api/devices/$DEVICEID  Authorization:"Bearer $TOKEN" \
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


