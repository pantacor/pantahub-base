---
title: Core Device Walkthrough
description: The Core Device services from device perspective
robots: nofollow, noindex
tags: walkthrough, CLI
breaks: false
---
# Core Walkthrough

## Connecting to the pantahub.com federation

As a device the owner will have made decisions about the federation,
the providers and the account details you have to use for communicating
with the scloud.

The key elements that user will have provided you as config are:

 1. The pantahub.com federation to use
 2. Your identity alongside your auth secret
 3. Your account id
 4. Your trails id
 
With that information you are ready to get started.

### Using the registry (LATER)

To login you will have to use the pantahub.com registry to look up the provider for your login id.

```
http https://reg.pantahub.com/resolve id=$login-id

HTTP 1.1 200 OK

{
	id: "$login-id",
	api: "prn:::login:/v1",
	endpoint: "https://login.pantahub.com/"
}
```

With that you know to us what login API to use and where the endpoint can be found.

### Log In

With the endpoint information you can log in

```
http GET "https://core.cloud.pantahub.com/api/v1/login" identity=$login-id secret=$secret
HTTP 1.1 200 OK
Set-Cookie: X-pantahub-core-access_token=axcqwe12easc12e12e3123123asd123, Path=/api/v1 
Set-Cookie: X-pantahub-core-refresh_token=1231asd123123qd12e12e Path=/api/v1

{
	status: 200,
	access_token: "axcqwe12easc12e12e3123123asd123"
	refresh_token: "1231asd123123qd12e12e"
	text: "Login Successful."
}
```

You can choose to use your http client cookie store facility or manually remember the access and refresh tokens and provide these i the Authorization: Bearer XXXXXXXXXX heading for future requests to your identity provider.


## Privileged Resources (OAUTH2 - LATER)

In our case the device wants to talk to trails to post its initial factory configuration (or retrieve XXX: clarify if we want both routes).

As with all prn resources, first the client will have to resolve the provider endpoint and API used for the configured trail.

```
http https://reg.pantahub.com/resolve id=$trails-id

HTTP 1.1 200 OK

{
	id: "$trails-id",
	api: "prn:::trails:/v1",
	endpoint: "https://trails.pantahub.com/v1/"
	resource: "https://trails.pantahub.com/v1/123qsd123i1e1230123awd
}
```

Clients typically use the returned resource URL if the api matches their requirements.

For trails, the device typically is interested in getting the list
of queued jobs or reporting back progress.

Each service keeps a session cookie themselves so that only the first request needs a auth provider roundtrip.

First, client gets a redirect to auth location with session cookie set.

```
http GET https://trails.pantahub.com/v1/123qsd123i1e1230123awd/steps

HTTP 1.1 301 Not allowed
Set-Cookie: session=123123123123123123123123; Path=/; Expires=Wed, 20 Jun 2016
Location: https://auth.pantahub.com/v1/auth?code=asdq12e9qwdo123o123123
```

Client honors redirect and obtains an auth_token from the service; callback url and scopes wanted have been communicated to the service and will be remembered through the code.

```
http GET https://auth.pantahub.com/v1/auth?code=asdq12e9qwdo123o123123

HTTP 1.1 301 Not allowed
Set-Cookie: session=123123123123123123123123; Path=/; Expires=Wed, 20 Jun 2016
Location: https://auth.pantahub.com/v1/auth?code=asdq12e9qwdo123o123123
```

## Privileged Resources (PROTOTYPE)

Accessing privileged resources of core services is done through
the Cookie set by /login or by providing the access_token used as
Authentication: Bearer <token>

For prototype simply always pass the session cookie to all core.cloud.pantahub.com request.

```
http GET https://core.cloud.pantahub.com/v1/objects/MYOBJECT
Cookie: X-pantahub-core-access_token=axcqwe12easc12e12e3123123asd123, Path=/api/v1 

HTTP 1.1 200 OK
{
... // json response for object MYOBJECT
}
```


## Downloading Objects

Downloading Objects can either be done by looking at the get_object
URL included in the json returned by the API call on the object resource,
or conveniently through redirect form the pseudo rest sub-endpoint ```download```

Example:

```
$ http GET https://core.cloud.pantahub.com/v1/objects/MYOBJECT/download

HTTP 1.1 301 Moved Temporarily
Location: http://direct-url.to.s3.amazon.com/presigned-secret

$ http --download http://direct-url.to.s3.amazon.com/presigned-secret
Downloading... DONE.
```

## Device Trails

Device Trails Core API allows a user to do asynchronous configuration management through the cloud using a stepwise approach.

Since devices might be temporarily or for extended period offline, you - as a device manager - can continue to draw a trail by adding a new step with changes to the trail.

The device posts its factory state on first boot and then will try to apply the changes along a trail of stepwise changes to the state configuration.

### First time announce

It is the devices responsibility to create it's trail and seed it with
the starting point configuration payload if the trail resource
does not yet exist.

By doing so it is established that the device can talk the cloud prototol and that it can be controlled through the trails API.

```
http POST https://core.cloud.pantahub.com/v1/trails/ \
    rev:=0 \
    kernel:='{ object: "prn:::objects:/MYKERNEL" }' \
    system:='{ object: "prn:::objects:/MYSYSTEM" }' \
    platform:='{ object: prn:::objects:/MYPLATFORM" }'

HTTP 1.1 200 OK
```
This will create a trail step for the factory state.

### Getting new steps

It is assumed that the client keeps a cache of at least 1 step locally queued up for processing and that it syncs back the QUEUED state for those steps that it plans to process and has locally stored yet.

Getting NEW steps:

```
http GET https://core.cloud.pantahub.com/v1/trails/DEVICEID/steps

HTTP 1.1 200 OK
X-REST-COUNT: 2
...

[
  {
    rev: 1
    ...
  },
  {
    rev: 2
    ...
  }
]
```

The device MUST report back to the cloud which items it has added to the QUEUE for processing, so the cloud can keep track of what steps can not be canceled anymore.

To get new steps, the client simply uses the trails steps resource
without any special parameters. The API will automatically default to just returning steps that are in NEW state.

A device client can also ask for the complete list starting at a given revision (for instance, the revision it has currently installed).

```
http GET https://core.cloud.pantahub.com/v1/trails/DEVICEID/steps?start=4

HTTP 1.1 200 OK
X-REST-COUUNT: 22
...

[
  { rev: 4, ... },
  { rev: 5, ... },
  { rev: 6, ... },
  ...
]
```

It is the clients obligation to add the step to its local queue that it plans to process, but before processing it change the step status in the cloud to QUEUED.

```
$ http PUT \
      https://core.cloud.pantahub.com/v1/trails/DEVICEID/steps/4 \
      status=QUEUED
$ http PUT \
      https://core.cloud.pantahub.com/v1/trails/DEVICEID/steps/5 \
      status=QUEUED
$ http PUT \
      https://core.cloud.pantahub.com/v1/trails/DEVICEID/steps/6 \
      status=QUEUED
$ http PUT \
      https://core.cloud.pantahub.com/v1/trails/DEVICEID/steps/7 \
      status=QUEUED
...
```
It is further the client responsibility to keep track of its current state revision and not apply changes that might not be a direct incremental step forward. If non such exists yet the device waits rather than stepping to a potential revision after that.

```
if queue.first().rev != self.rev+1 {
	queue.forAll().setStatus("WONTGO")
	queue.empty()
}
```

### Reporting Progress

The device has to communicate changes to state at the first possible
moment for steps.

For long running steps the client is also encouraged to send 
progress information.

For error cases the device must attache the relevant logs and debug information like traces, core dumps to the step and include an error
code and error message.

```
$ http PUT https://core.cloud.pantahub.com/api/v1/trails/DEVICEID/steps/4
    status=INPROGRESS \
    progress=50 \
    message="...processing..." \
    error="" \
    log="" \
    debugfiles:={}
```

### Reporting ERRORS

The device must report errors at the very first moment after encountering these exceptional states.

```
$ http PUT https://core.cloud.pantahub.com/api/v1/trails/DEVICEID/steps/4
    status=ERROR \
    progress=50 \
    message="...processing..." \
    error="" \
    log="" \
    debugfiles:={}
```

### Continuous operation

During the lifecycle of a device the device listens for new steps getting posted, enqueues them and report back status, progress and errors as it walks along the trail.

If an error is encountered during application of a change, the previous state is preserved, the failed step is set to ERROR state and device will discard already queued steps after this until the trail is cleared from steps in ERROR state.

Pseudo-Code for a client:
```
while true:
  queue.sync_open(steps)
  if !queue.empty():
    step = queue.drain()
    if step.status == ERROR:
      queue.clear()
    else if !machine.do(step):
      queue.clear()
  sleep (abit)
```	

### Processing ABORTS

The device steps list will not ony include NEW steps, but also 
steps with abort flag set.

Device must cancel jobs that it has locally QUEUED if aborted
and change the device-abort flag to true and set the step status to ABORTED and continue to process follow up steps.

Device must keep device-abort flat set to false in case the step was applied successfully when it got to know about the abort wish. This will allow higher level tools to apply manual backout operations (like a reverse change added to the trail).

### Factory Reset
In case the device thinks it is in first boot, but the resource already
exists, this indicates either an error or a factory reset.

To allow factory reset the user has to DELETE the trail first.

**User** sets device back into registration mode in the cloud:

```
http --session user \
	DELETE https://core.cloud.pantahub.com/v1/trails/DEVICEID

HTTP 1.1 OK
```

Now the **Device** can post a new factory state.
