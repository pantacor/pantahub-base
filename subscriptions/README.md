# WARNING (alpha API and code)

This is alpha API and code; do not use it from third party software that
you are not willing to rewrite or throw away very soon and often...

You have been warned :).


# Configure admin users in pantahub

By default we assign the demouser "admin" permissions to grant subscriptions for pantahub.

You can change that through adding a comma separated list of prns for the following env
configurations. The following are the defaults we use:

```
	PANTAHUB_ADMINS="prn:pantahub.com:auth:/admin"
	PANTAHUB_SUBSCRIPTION_ADMINS=""
```

Remember that you have to set password explictely if you want to enable the admin demo user.

To do define the admin password as env also with:


```
	PANTAHUB_DEMOACCOUUNTS_PASSWORD_admin=YOURPASSWORDHERE
```

# Set Subscription (as Admin user)

First log in as admin user, e.g.

```
TOK=`http POST https://api.pantahub.com/auth/login username=admin password=YOURPASSWORDHERE | jq  -r .token`
```

Then you can create or update the subscription plan for any given prn using the following rest call:

```
http PUT https://api.pantahub.com/subscriptions/admin/subscription \
	plan=prn::subscriptions:VIP \
	Authorization:" Bearer: $TOK"
```

Right now the following plans are available to choose from:

```
	prn::subscriptions:FREE
	prn::subscriptions:VIP
	prn::subscriptions:CUSTOM
```

You can overwrite default properties of plan through a json map that you can pass in as 'attrs' argument:

```
http PUT https://api.pantahub.com/subscriptions/admin/subscription \
	plan=prn::subscriptions:VIP \
	attrs:='{"BANDWIDTH": "100GiB"}' \
	Authorization:" Bearer: $TOK"
```

The effects should be visible for users right away in the UI and on dash endpoint.

# See your subscription details (as user)

First login as normal user, e.g. with "user1" demoaccount:

```
TOK1=`http POST https://api.pantahub.com/auth/login username=user1 password=YOURUSER1PASSWORT | jq -r .token`
```

Next you can get your subscription status through simple GET against the subscriptions main endpoint:

```
ttp GET https://api2.pantahub.com/subscriptions/ Authorization:" Bearer $TOK1"
HTTP/1.1 200 OK
Connection: keep-alive
Content-Encoding: gzip
Content-Type: application/json; charset=utf-8
Date: Mon, 05 Mar 2018 20:54:40 GMT
Server: nginx/1.13.5
Strict-Transport-Security: max-age=15724800; includeSubDomains;
Transfer-Encoding: chunked
X-Powered-By: go-json-rest

{
    "Page": -1,
    "Size": 1,
    "Start": 0,
    "Subs": [
        {
            "attr": {
                "BANDWIDTH": "100GiB",
                "DEVICES": "100",
                "OBJECTS": "20GiB"
            },
            "history": [
                {
                    "attr": {
                        "BANDWIDTH": "100GiB",
                        "DEVICES": "100",
                        "OBJECTS": "20GiB"
                    },
                    "id": "5a9dab7a9764eb000731cc60",
                    "issuer": "prn:pantahub.com:auth:/admin",
                    "last-modified": "2018-03-05T20:42:09.2Z",
                    "prn": "prn::subscriptions:/5a9dab7a9764eb000731cc60",
                    "service": "prn::subscriptions:",
                    "subject": "prn:::accounts:/59ef9e241e7e6b000d3d2bc7",
                    "time-created": "2018-03-05T20:41:30.58Z",
                    "type": "prn::subscriptions:VIP"
                }
            ],
            "id": "5a9dab7a9764eb000731cc60",
            "issuer": "prn:pantahub.com:auth:/admin",
            "last-modified": "2018-03-05T20:42:22.047Z",
            "prn": "prn::subscriptions:/5a9dab7a9764eb000731cc60",
            "service": "prn::subscriptions:",
            "subject": "prn:::accounts:/59ef9e241e7e6b000d3d2bc7",
            "time-created": "2018-03-05T20:41:30.58Z",
            "type": "prn::subscriptions:VIP"
        }
    ]
}
```

# See your quotas on the dash endpoint

Simply log in as normal user like in section above and then query dash api:

```
$ http GET https://api2.pantahub.com/dash/ Authorization:" Bearer $TOK1"
HTTP/1.1 200 OK
Connection: keep-alive
Content-Encoding: gzip
Content-Type: application/json; charset=utf-8
Date: Mon, 05 Mar 2018 20:55:53 GMT
Server: nginx/1.13.5
Strict-Transport-Security: max-age=15724800; includeSubDomains;
Transfer-Encoding: chunked
X-Powered-By: go-json-rest

{
    "nick": "user1",
    "prn": "prn:::accounts:/user1",
    "subscription": {
        "billing": {
            "AmountDue": 0,
            "Currency": "USD",
            "Type": "Monthly",
            "VatRegion": "World"
        },
        "plan-id": "VIP",
        "quota-stats": {
            "BANDWIDTH": {
                "Actual": 0,
                "Max": 100,
                "Name": "BANDWIDTH",
                "Unit": "GiB"
            },
            "DEVICES": {
                "Actual": 1,
                "Max": 100,
                "Name": "DEVICES",
                "Unit": "Piece"
            },
            "OBJECTS": {
                "Actual": 0.06,
                "Max": 20,
                "Name": "OBJECTS",
                "Unit": "GiB"
            }
        }
    },
    "top-devices": [
    ]
}
```


Have fun! 

