---
title: Core User Walkthrough
description: The Core Device services from user CLI perspective
robots: nofollow, noindex
tags: walkthrough, CLI
breaks: false
---
# Core Walkthrough for Users

## Get Started

To get started download the scloud tool for your platform.

Instructions can be found on scloud.pantahub.com/getting-started

At first you have to tell scloud which service registry it should use. The default registry is at https://reg.pantahub.com

```
$ scloud init https://reg.pantahub.com
Welcome to the new world! This service is provided to you by 
pantahub.com. For more info and service/support visit

     http://www.pantahub.com

Enjoy your stay!
```

Now that you have joined the pantahub.com federation you are ready to go.


### Registering a new user

First you need a user; for that use the default scloud user app:

```
$ scloud user register
Username: mynewuser
Email: email@my.tld
Password: XXXXXXXXXX
Repeat Password: XXXXXXXXXX

User Account Requested!
To activate, follow instructions you received via email within 12h

```

### Login as a user

Now that you have an account, you can login:

```
$ scloud login
You are about to log in to "prn:pantahub.com:login:v1:/user"
Username: mynewuser
Password: XXXXXXXXXX
.
Success. Welcome 'mynewuser'
```

### Whoami

Since you can have multiple accounts with multiple providers you can use the whoami command to find out which account you are currently logged in with and what your default service providers for the scloud subcommands are.

You can change the providers with your accounts registry and for one time command execution you also use the --provider flag.

```
$ scloud whoami
Username: yourusername
Global ID: prn:pantahub.com::login:/yourusername
```

### ADVANCED: Apps

scloud subcommands are mapped to microservice REST APIs for which you
can choose to use a third party provider if needed.

By default scloud will be configured to use the default app providers
by pantahub.com. 

```
$ scloud apps
login = prn:::login:v1:/user
accounts = prn:::account-registry:/v1/
devices = prn:::device-registry:/v1/
objects = prn:::objects-s3:/v1/
trails = prn:::device-trails:/v1/   # XXX: trails not be part of scloud?
```

You can change this through the ```apps``` command.


```
$ scloud apps devices=prn::third-party:device-registry:/v1/
Trying to log into 'prn::third-party:device-registry:/v1/'
Not federated.

Please visit the following URL with your browser to sign up:

   https://devives.third-party.tld/signup?client="asdasd1231kasdikawdio31239asd91239123"

Waiting...
Waiting...

Thanks. Your new devices app has been set up...
```

The change should be reflected in your scloud configuration now:

```
$ scloud apps
login = prn:::login:v1:/user
accounts = prn:::account-registry:/v1/
devices = prn::third-party:device-registry:/v1/
objects = prn:::objects-s3:/v1/
trails = prn:::device-trails:/v1/   # XXX: trails not be part of scloud?
```

## Accounts

scloud is a multi tenant, multi account solution.

This means that for any given identity there can be multiple accounts. Those accounts can be hosted at one or many account providers.

When operating on services the user has to provide the account in the context of his operations. 

For instance the asac@pantahub.com identity has multiple accounts:

 1. user account at accounts.pantahub.com
 2. org account at accounts.pantahub.com
 3. org account at accounts.third-party.tld
 
Each account has global unique id that the identity provider assert. 

### Create an account

To create an account user will visit the account provider website with his identity provider identity and sign up.

Account information can then be edited by the user and services that need user information can federate with the account provider using oauth2.

```
$ scloud accounts-create prn:::accounts:/users/ --nick default
To setup your account, please visit and follow instructions at:

   https://accounts.pantahub.com/accounts/create

Created 'default' user account prn:::accounts:/users/1209asd91239123123
```

The nick is a personally unique shorthand for selecting the account you want to use for API calls.

The accounts provider prn can be omitted if you want to use the default provider for the 'accounts' app configured in scloud.

### Listing accounts

To list accounts for your identtiy at a given provider you use the ```accounts``` subcommand. scloud will use your default provider configured if non is given.

```
$ scloud accounts
Looking up accounts @ prn:::accounts:/users/
'default' prn:::accounts:/users/1209asd91239123123
'myorg' prn:::accounts:/orgs/1awd12easd1212
```

### Account details

```
$ scloud accounts 'default'
Type: Individual
ID: prn:::accounts:/users/1209asd91239123123
Name: Mister Louis
Description: Hi, I am Louis, King of the Hill,
| my main interest is in making devices that rock.
| You can find me on github and on G+.
Identity: prn:::identity:/luoisxiv
```


## Device Registr

The Device Registry is the central place where a user maintains the list of his devices including metadata reported from devices as well as manually supplemented.

The user api for device registry specification is prn::pantahub.com:

### Creating a device

To create a device you own and have control over is straight forward.

```
$ scloud devices create <yourdevicenick>
Success!!
Your device '<yourdevicenick>' can now use scloud.
Use the following token so we can identify it:

  x8123asc123123c123123c123123123qw123123
```

### List Devices

You can get a list of your devices:

```
$ scloud devices
Device-ID             Nick                  Last-Seen
=====================================================
YOURDEVICEID-1        yourdevicenick1       NEVER
YOURDEVICEID-2        yourdevicenick2       20min ago
```

If your device has never talked to our cloud it might mean that you haven't set up the device client correctly. Remember to configure your client to use the auth token you got when creating the device.

### Device Info

Once your device has signed in to the cloud, it will announce meta information about the system hardware and software stack as well as configuration details.

One key element of system information is the info about which cloud APIs it supports and which provider it uses for each.

These are important so that client including the scloud CLI can use the right provider for individual APIs

```
 $ scloud devices info YOURDEVICEID-1
 Registry: https://reg.pantahub.com
 Meta Info:
   MachineInfo: DT Router XYZ
   HardwareInfo: ARMv7, ...
 Core APIs:
   - prn::registry:/apis/device-login/v1
      => prn:::login:/device/login/v1
   - prn::registry:/apis/devices/v1
      => prn:::device-registry:/v1/
   - prn::registry:/apis/objects/v1
      => prn:::objects-s3:/v1
   - prn::registry:/apis/device-trails/v1
      => prn:::device-trails:/v1/
```

## Device Login

Device Login API is currently identical with User Login API except that the JWT will include hints that this account type is a device account and who the responsible account for this device is.

In future we will move to oauth and the Login process for devices will then go like this:

1. Device initiates request to protected resource (for instance /me on device registry)
2. Resource owner establishes a token, but has no info about the identity of the user.

## Objects

Device Objects API allows to retrieve and store protected artifacts and delegate access to devices and third party apps. Objects API is the heart of cloud file storage for kernels, system, platform and apps.

To store an object you use the objects subcommand of scloud:

```
scloud objects-add -t application/x-octet-stream -v private|public -f localkernel-file -m "dt box kernel 1.3.4" /kernels/
New object created:
 - prn::pantahub.com:objects:/kernels/123zxqsd123
 - Size: 16M
 - Visibilty: public|private
 - Last-Modified: XXXXXXX
 - sha256sum: xasd09123asd9asd91239c90sdc9123912309as12390123
```

To see all your objects in a list:

```
$ scloud objects
/kernels/123zxqsd123 16M private "dt box kernel..."
/platforms/123asd9213123 10M private "openwrt example 1.11 for DT box"
/systemv/123asd9213123 580K private "systemv for dtbox"
```

To mark an object as deleted:

```
$ scloud object-delete /kernels/121321xxxxxxx
Marked deleted.
```

## Device Trails

Device Trails Core API allows a user to do asynchronous configuration management through the cloud using a stepwise approach.

Since devices might be temporarily or for extended period offline, you - as a device manager - can continue to draw a trail by adding a new step with changes to the trail.

Device will report status, progress and errors as it "walks" along the trail. If errors are detected when applying and health_checking the change, the previous state will be preserved. and the trail will be in ERROR state - not continue.

Users and higher level tools can discard error steps and safely append different steps.


### Your Device Trails - Overview

Let't take a look at our device trails...

```
 $ scloud device-trails
 Device ID        Goal       Device    Status              Last-Seen
 =====================================================================
 YOURDEVICEID-1   rev1       rev1      IN-SYNC             NOW
 YOURDEVICEID-2   rev4       rev1      INTRANSIT(->2)      10min ago
 YOURDEVICEID-3   rev3       rev2      ERROR(@3)           22min ago
```

This tells you that

* Device 1 is currently in-sync with the configuration goal (being the HEAD of the trail)
* Device 2 has to catch up and is right now stepping along the trail from revision 1 to 4.
* Device 3 however is stuck at revision 2 because the step to 3 caused an error


### Looking at a Device Trail

Lets look at the trail to see what that means:

```
$ scloud device-trails YOURDEVICEID-2
    1: FACTORY INSTALL     [CURRENT]
    2: UPDATE-APP-X        [INPROGRESS 75%]
    3: UPDATE-KERNEL TO Y  [QUEUED]
    4: UPDATE-KERNEL TO Z  [NEW]
```

### Looking at a Device Trail Step

You can inspect each step as well.

```
$ scloud device-trails YOURDEVICE-2 step 2
Device-Progress: 75% (NEW|QUEUED|0-100%|DONE|ERROR|WONTGO)
Device-Abort: n
Device-Note: "* downloaded object [DONE]\n* unpacked object [DONE]\n* health check run [DONE]\n* finalize install [TODO]"
Device-Log: ""
Step-Abort: n
Step-Note: Update Kernel to Y
Step-Committer: prn::pantahub.com:accounts:/yourusername"
Step-Patch-RFC7386:
{
   revision: 2,
   software: {
      kernel: {
        object: "prn::pantahub.com:objects:/ab91234asd912312391235"
        auth-token: "a91239asd9asd9asd91239123912312391239123"
      }
   }
}
Step-Full:
{
   revision: 2,
   software: {
      kernel: {
        object: "prn::pantahub.com:objects:/ab91234asd912312391235"
        auth-token: "a91239asd9asd9asd91239123912312391239123"
      },
      system: {
        object: "prn::pantahub.com:objects:/ab91234asd9123123912123"
        auth-token: "a91239asd9asd9asd912391239123123912aa9123"
      },
      platform: {
        object: "prn::pantahub.com:objects:/ab91234asd912312391235"
        auth-token: "a91239asd9asd9asd912391239123123912bbb123"
      }
   }
}
```

For details about the fields and how they are set/used/read, please consult the Trails API SPEC.

### Adding a new Step

You can add a new step. Let's upgrade the kernel

```
$ scloud device-trails YOURDEVICE-2 step-add \
    -m "new kernel with fixes" \
    --RFC7386 {
       revision: 5,
       software: {
          kernel: {
            object: "prn::pantahub.com:objects:/kernels/123zxqsd123"
            auth-token: "a91239asd9asd9asd91239123912312391239121"
          }
       }
    }"
```

Important: revision must be exactly a +1 increment from current head. This allows device-trails services to ensure there are no concurrency issues. First comes first serves...

### Retrying a failed step

If you think it was an issue due to exceptional environmental circumstances (like powerloss during upgrade), you can retry:

```
$ scloud device-trails retry YOURDEVICE-3
```

This will set the walk steps to NEW. 

Device will see them as if they have just been added and process them accordingly.

A retry counter will be increased for the step that ERRORED intially allowing higher level tools to develop smart retry heuristic and scheduling algorithms.

```
# XXX: awful CLI; needs redesign
$ scloud device-trails info YOURDEVICE-3
INPROGRESS: Step 2 @ 60%
Steps:
    2: UPDATE-APP-X        [60%]
    3: UPDATE-KERNEL TO Y  [NEW](RETRIES: 1)
    4: UPDATE-KERNEL TO Z  [NEW]
```

### Rerouting your Device

If you have identified and fixed an issue that you feel caused the issues with the step, you need to reroute your trail. To reroute your device you simply delete all the steps that are in ERROR and WONTGO state and then add your new step. For convenience all of this is available as a convenience reroute command:

```
$ scloud device-trails reroute YOURDEVICE-3 \
    -m "workaround problem in rev 3" \
    "{
       revision: 3,
       software: {
          kernel: {
            object: "prn::pantahub.com:objects:/ab91234asd912312391244"
            auth-token: "a91239asd9asd9asd91239123912312391239111"
          }
       }
    }"
ACK. Rerouting device:
- Deleting WONTGO step 4: UPDATE TO Z
- Deleting FAILED step 3: UPDATE-KERNEL to Y
- Adding new step 3: workaround problem in rev 3
```

After this your device see the new step as if the others never existed and will try to move along the new, rerouted trail!

### Aborting steps

Steps can be aborted as an emergency measure. Due to the asynchronous nature of the device there are two ways this alert flag will be honored by clients:

1. change has not yet been applied and can be aborted if started.
2. device only notices abort after the change got successfully applied. 

In case 1. the device will stop applying the change, leaves the running system at the previous state

In case 2. device will move trail state to DONE, but keep step-abort flag as "y" and device-abort flag as "n" indicating that we have a task at hand for which an abort was requested, but couldn't be accommodated by the device which finished processing this step already. 

User can still change the step-abort from y to n to reflect that the error condition has been dealt with in some other way (e.g. higher level tool added a reverse commit to the end of the trail).

