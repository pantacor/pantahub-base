# Healthz API

This is a simple rest endpoint API directed towards devops that 
need a way to check for readiness and liveliness of the system during
operation to manage the service lifecycle smartly.

This API endpoint is NOT meant to be used by devices or users.

Only principal that has access to this endpoint is the "saadmin" endpoint
which must be authenticated through Basic Auth with the password configured
by devops using the PANTAHUB_SA_ADMIN_SECRET.

If that secret is not set, this endpoint cannot be used.

## Example 1

We start pantahub with PANTAHUB_SA_ADMIN_SECRET set to 'test1234':

```
$ PANTAHUB_SA_ADMIN_SECRET=test1234 pantahub-base 
...
```

And then use the following json/rest call to get info about
healthz:


```
http --auth saadmin:123123 localhost:12365/healthz/
HTTP/1.1 200 OK
Content-Length: 80
Content-Type: application/json; charset=utf-8
Date: Thu, 29 Jun 2017 21:50:28 GMT
X-Powered-By: go-json-rest

{
    "code": 0,
    "duration": 2706206,
    "start=-time": "2017-06-29T23:50:28.09254266+02:00"
}
```

Any status code greater or equal than 400 should be interpreted as failed.

