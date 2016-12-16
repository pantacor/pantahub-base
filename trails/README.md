# Trails

Trails allow to users to change device configurations asynchronously using a step wise approach.

Trails API hence has two views with different priliges on it:

 1. Device accounts
 2. User accounts

## Creating a trail.

Trails can be created as users or device on first boot.

To create a trail log in with a device acccount (see auth api documentation).

For this REAME we assume that you saved a device token in the ```DTOKEN```
environemtn variable and a user token in the ```UTOKEN``` env.

To create a trail we use the POST method on the trails api top level element
using the DTOKEN:

```
http POST localhost:12365/api/trails/ Authorization:"Bearer $DTOKEN" \
	kernel:='{ "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000001"}' \
	app:='{"object": "prn:pantahub.com:objects:/57b6fa88c094f67942000002"}'

HTTP/1.1 200 OK
Content-Length: 419
Content-Type: application/json; charset=utf-8
Date: Sat, 27 Aug 2016 22:04:31 GMT
X-Powered-By: go-json-rest

{
    "device": "prn:pantahub.com:auth:/device1", 
    "factory-state": {
        "app": {
            "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000002"
        }, 
        "kernel": {
            "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000001"
        }
    }, 
    "id": "57c20e6fc094f6729b000001", 
    "last-seen": "0001-01-01T00:00:00Z", 
    "last-step": "2016-08-28T00:04:31.386929333+02:00", 
    "last-walk": "2016-08-28T00:04:31.386929333+02:00", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "tail": 0, 
    "tip": 0
}

```

## Getting Steps (User Account)

Taking a peek at the steps as a user will by default return
all steps that are not completed.

As a device by defaults only those steps get returned that
are in state NEW.

All other states can be queried with query parameter q=<STATE>|ALL

Lets see as a user:

```
http GET localhost:12365/api/trails/57c20e6fc094f6729b000001/steps Authorization:"Bearer $UTOKEN"
HTTP/1.1 200 OK
Content-Length: 424
Content-Type: application/json; charset=utf-8
Date: Sat, 27 Aug 2016 22:05:07 GMT
X-Powered-By: go-json-rest

[
    {
        "commit-msg": "Factory State (rev 0)", 
        "committer": "", 
        "device": "prn:pantahub.com:auth:/device1", 
        "id": "57c20e6fc094f6729b000001-0", 
        "owner": "prn:pantahub.com:auth:/user1", 
        "progress": {
            "log": "", 
            "progress": 0, 
            "status": "DONE", 
            "status-msg": ""
        }, 
        "rev": 0, 
        "state": {
            "app": {
                "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000002"
            }, 
            "kernel": {
                "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000001"
            }
        }, 
        "trail-id": "57c20e6fc094f6729b000001"
    }
]
```

Here we see the intiial factory step being added to the system.

## Adding steps

Steps need to be added with appropriate incremental rev to ensure that no 
concurrently added steps can cause incontinuity in the sequence of steps.

Lets simulate an app update as our rev 1:

```
http POST  localhost:12365/api/trails/57c20e6fc094f6729b000001/steps Authorization:"Bearer $UTOKEN" \
	rev:=1 \
	commit-msg="Update App to new Release" \
	state:='{
	          "kernel": {"object": "prn:pantahub.com:objects:/57b6fa88c094f67942000001"},
	          "app":    {"object": "prn:pantahub.com:objects:/57b6fa88c094f67942000003"}
			}'

HTTP/1.1 200 OK
Content-Length: 411
Content-Type: application/json; charset=utf-8
Date: Sat, 27 Aug 2016 22:22:14 GMT
X-Powered-By: go-json-rest

{
    "commit-msg": "Update App to new Release", 
    "committer": "", 
    "device": "prn:pantahub.com:auth:/device1", 
    "id": "57c20e6fc094f6729b000001-1", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "progress": {
        "log": "", 
        "progress": 0, 
        "status": "NEW", 
        "status-msg": ""
    }, 
    "rev": 5, 
    "state": {
        "app": {
            "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000003"
        }, 
        "kernel": {
            "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000001"
        }
    }, 
    "trail-id": "57c20e6fc094f6729b000001"
}
```

## Accessing Individual Steps

To access individual steps relative to the trail you use "rev" in the path:

```
http GET localhost:12365/api/trails/57c20e6fc094f6729b000001/steps/1 Authorization:"Bearer $UTOKEN"
HTTP/1.1 200 OK
Content-Length: 422
Content-Type: application/json; charset=utf-8
Date: Sat, 27 Aug 2016 22:45:59 GMT
X-Powered-By: go-json-rest

{
    "commit-msg": "Update App to new Release", 
    "committer": "", 
    "device": "prn:pantahub.com:auth:/device1", 
    "id": "57c20e6fc094f6729b000001-0", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "progress": {
        "log": "", 
        "progress": 0, 
        "status": "NEW", 
        "status-msg": ""
    }, 
    "rev": 0, 
    "state": {
        "app": {
            "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000003"
        }, 
        "kernel": {
            "object": "prn:pantahub.com:objects:/57b6fa88c094f67942000001"
        }
    }, 
    "trail-id": "57c20e6fc094f6729b000001"
}
``` 

## Device Progress Postings

To post progress, devices will PUT to the pseudo "progress" node under each
step. In this case our device wants to confirms that it has seen the newly
requested step and that it is acting on it.

```
http PUT localhost:12365/api/trails/57c20e6fc094f6729b000001/steps/progress Authorization:"Bearer $DTOKEN" \
	log = "" \
	progress = 0 \
	status = "QUEUE" \
	status-msg  = ""

HTTP/1.1 200 OK
Content-Length: 57
Content-Type: application/json; charset=utf-8
Date: Sat, 27 Aug 2016 22:50:29 GMT
X-Powered-By: go-json-rest

{
    "log": "", 
    "progress": 0, 
    "status": "QUEUE", 
    "status-msg": ""
}
```

