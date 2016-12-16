---
title: Basic Fleet (TM) Walkthrough
description: Free DevOps for Devices
robots: nofollow, noindex
tags: walkthrough, CLI
breaks: false
---


# Basic Fleet Walkthrough

basic-fleet.app.pantahub.com is an example app that shows how third party apps can add value by using the pantahub cloud APIs.

Basic Fleet is available on github and the docs there explain how write such app.

To enable use basic-fleet app with your pantahub.com account, you need to grant the app read access to your devices in the registry and have to grant user access to your devices trails.

The Basic Fleet can manage devices that are using Device Trails API v1 for configuration management and the app does not require a special API or agent.

## Getting Started

### Getting the CLI app

The basic-fleet app comes with its own CLI and uses standard account and auth provider for identification. A web frontend is planned.

The CLI app is a single binary and can be downloaded through wget basic-fleet.app.pantahub.com/CLI/pantahub-fleet

### Logging In

Once downloaded you have to first log in and provide your full prn user id. basic-fleet app will then use your login provider to autenticate you.

```
$ basic-fleet login prn::pantahub.com:auth/user1
Please open the following URL in your provider and follow the instructions.

 Open: https://auth.pantahub.com/login?token=asd123asd9asdf123rj124123912319123asd&callback=.....

... Waiting ... (Abort attempt with Ctrl-C)
... Waiting ... (Abort attempt with Ctrl-C)
... Waiting ... (Abort attempt with Ctrl-C)
...
Authenticated as 'auth/user1.
```

## Managing devices

basic-fleet does not directly communicate with devices. It indirectly manages a users fleet by accessing the users device registry and the Device Trails provider on behalf of the user.

The user can make fine grained decision which devices the basic-fleet app
can see and manage.

For that all pantahub cloud certified auth providers offer a resource selector for the auth consent screen that in our case enables the user to make a decision which devices basic-fleet can see.

For that the device registry has to implement the standard pantahub-auth-resource-select API and must be properly registered with the PANTAHUB.xyz registry.

### Import Devices from Device Registry

To entrust management of some or all of your devices you use the add-devices command.

```
$ basic-fleet add-devices
Please open the following URL in your browser to select the devices you want to fleet manage:

   https://auth.pantahub.com/rsel_login?api=prn::pantahub.xzy:api:/devices/v1&token=...........&select

... Waiting .... (Abort attempt with Ctrl-C)
... Waiting .... (Abort attempt with Ctrl-C)
...

Thanks for entrusting us the following devices:

DEVICEID-1@cisco.com "bathroom-wifi"
DEVICEID-2@cisco.com "shower-wifi"
DEVICEID-3@cisco.com "gardenleft-wifi"
DEVICEID-4@cisco.com "gardenright-wifi"
DEVICEID-5@cisco.com "gardenpool-wifi"
DEVICEID-7@cisco.com "living-wifi"
DEVICEID-8@cisco.com "attic-wifi"
DEVICEID-9@pantahub.com "devbox1-wifi"
DEVICEID-A@pantahub.com "devbox2-wifi"
DEVICEID-B@pantahub.com "devbox3-wifi"
DEVICEID-C@pantahub.com "devbox4-wifi"

You can now use basic-fleet to organize and manage these. Try auto-fleet to get a quick start...
```

### Listing devices

All devices under management can be seen using the ```devices``` subcommand.

```
$ basic-fleet devices
DEVICEID-1@cisco.com    "bathroom-wifi"
DEVICEID-2@cisco.com    "shower-wifi"
DEVICEID-3@cisco.com    "gardenleft-wifi"
DEVICEID-4@cisco.com    "gardenright-wifi"
DEVICEID-5@cisco.com    "gardenpool-wifi"
DEVICEID-7@cisco.com    "living-wifi"
DEVICEID-8@cisco.com    "attic-wifi"
DEVICEID-9@pantahub.com    "devbox1-wifi"
DEVICEID-A@pantahub.com    "devbox2-wifi"
DEVICEID-B@pantahub.com    "devbox3-wifi"
DEVICEID-C@pantahub.com    "devbox4-wifi"
```

## Device Rollouts

Changes to the fleet are done through "rollouts".

To look at active rollouts you can use the rollouts command.

```
$ basic-fleet rollouts
CHANGE ace1df "devices.csv" PROCESSING A:10 D:6 E:1 T:3
CHANGE b1efd7 "canary.csv" DONE A:2 D:2 E:0 T:0
CHANGE bde7d5 "10percent.csv" DONE A:2 D:2 E:0 T:0
CHANGE 91bda3 "canary.csv" DONE A:2 D:2 E:0 T:0
```

To start a new rollout you use the addrollout subcommand

```
$ basic-fleet addrollout 10percent.csv platform:=prn::pantahub.com:objects:/123asd91234jsdf9i
New rollout '81ef7ab' to 6 devices.
```

To look Look at a rollout:

```
$ basic-fleet rollouts 81ef7a
Change: platform:=prn::pantahub.com:objects:/123asd91234jsdf9i
DEVICEID-1@cisco.com  "bathroom-wifi"     [OK]
DEVICEID-2@cisco.com  "shower-wifi"       [QUEUE]
DEVICEID-3@cisco.com  "gardenleft-wifi"   [75%]
DEVICEID-4@cisco.com  "gardenright-wifi"  [ERROR: Segmentation Fault]
DEVICEID-5@cisco.com  "gardenpool-wifi"   [OK]
DEVICEID-7@cisco.com  "living-wifi"       [QUEUE]
DEVICEID-8@cisco.com  "attic-wifi"        [OK]
```

## Device Backouts

In case your rollout goes bad and you see significant failures it might be time to think about backing out
a rollout.

Backing out will
1. Cancel all scheduled "Change" workers and jobs still in the queue for the rollout.
2. Revert devices that already updated successfully by adding a rollout changing back to the previous versions on top
3. ABORT changes currently in progress if the previous state can be perserved
4. ROLLBACK to get establish state from before the change.
5. report how the ABORT got resolved as either ABORT or RABORT status (in case a rollback had to be done).

OK, so much for the theory. Let's do a backout:

```
$ basic-fleet rollouts 81ef7a backout
New rollout '12baf9' backing out '81ef7a'.
```

Looking at the backed out rollback with setting the trail ```step-ABORT``` yes flag:

```
$ basic-fleet rollouts 81ef7a
Change: platform:=prn::pantahub.com:objects:/123asd91234jsdf9i
Status: ABORTING
Abort: yes
DEVICEID-1@cisco.com  "bathroom-wifi"     [QUEUE, ABORT]
DEVICEID-2@cisco.com  "shower-wifi"       [OK]
DEVICEID-3@cisco.com  "gardenleft-wifi"   [OK]
DEVICEID-4@cisco.com  "gardenright-wifi"  [OK]
DEVICEID-5@cisco.com  "gardenpool-wifi"   [ERROR, ABORT]
DEVICEID-7@cisco.com  "living-wifi"       [OK]
DEVICEID-8@cisco.com  "attic-wifi"        [75%, ABORT]
```

On top a backout-rollout is posted that would apply the reverse changes for devices that already finished the upgrade.

```
$ basic-fleet rollouts 12baf9
Change: platform:=prn::pantahub.com:objects:/123asd91234jsdf9i
Status: INPROGRESS
Abort: no
Devices:
  DEVICEID-1@cisco.com  "bathroom-wifi"     [QUEUE]
  DEVICEID-2@cisco.com  "shower-wifi"       [OK]
  DEVICEID-3@cisco.com  "gardenleft-wifi"   [OK]
  DEVICEID-4@cisco.com  "gardenright-wifi"  [OK]
  DEVICEID-5@cisco.com  "gardenpool-wifi"   [QUEUE]
  DEVICEID-7@cisco.com  "living-wifi"       [OK]
  DEVICEID-8@cisco.com  "attic-wifi"        [75%]
```

Once finished the backout rollout will indicate how the ABORTs got resolved

```
$ basic-fleet rollouts 81ef7a
Change: platform:=prn::pantahub.com:objects:/123asd91234jsdf9i
Status: ABORTED
Abort: yes
Devices:
  DEVICEID-1@cisco.com  "bathroom-wifi"     [ABORT]
  DEVICEID-2@cisco.com  "shower-wifi"       [OK]
  DEVICEID-3@cisco.com  "gardenleft-wifi"   [OK]
  DEVICEID-4@cisco.com  "gardenright-wifi"  [OK]
  DEVICEID-5@cisco.com  "gardenpool-wifi"   [ABORT]
  DEVICEID-7@cisco.com  "living-wifi"       [OK]
  DEVICEID-8@cisco.com  "attic-wifi"        [ROLLBACK]
```

In this case the gardepool-wifi changed was rolled back as the install already finished when the abort instruction was received by the device.

```
$ basic-fleet rollouts 12baf9
Change: platform:=prn::pantahub.com:objects:/123asd91234jsdf9i
Abort: no
Status: DONE
Devices:
  DEVICEID-1@cisco.com  "bathroom-wifi"     [OK]
  DEVICEID-2@cisco.com  "shower-wifi"       [OK]
  DEVICEID-3@cisco.com  "gardenleft-wifi"   [OK]
  DEVICEID-4@cisco.com  "gardenright-wifi"  [OK]
  DEVICEID-5@cisco.com  "gardenpool-wifi"   [OK]
  DEVICEID-7@cisco.com  "living-wifi"       [OK]
  DEVICEID-8@cisco.com  "attic-wifi"        [OK]
```

Finally also the revert installs finished and device is operation with a change backed out mid OTA. Cool!

## Device Errors

If your device has an error you can get the device trails info about that error through the devices sub-command

```
$ basic-fleet errorinfo DEVICEID-1@cisco.com
Rollout: 81ef7a
Error: Segmentation Fault...
LogHead: "Cannot find index.\n Segmentation fault..."
```
