---
title: Bigger Fleet (TM) Walkthrough (NEW)
description: Free DevOps for Devices
robots: nofollow, noindex
tags: walkthrough, CLI
breaks: false
---


# Introduction

bigger-fleet.app.pantahub.com is an example app that shows how third party apps can add value by using the pantahub cloud APIs.

Bigger Fleet is available on github and the docs there explain how write such app.

To enable use bigger-fleet app with your pantahub.com account, you need to grant the app read access to your devices in the registry and have to grant user access to your devices trails.

The Bigger Fleet can manage devices that are using Device Trails API v1 for configuration management and the app does not require a special API or agent.

# Getting Started

## Getting the CLI app

The bigger-fleet app comes with its own CLI and uses standard account and auth provider for identification. A web frontend is planned.

The CLI app is a single binary and can be downloaded through wget bigger-fleet.app.pantahub.com/CLI/pantahub-fleet


## Logging In

Once downloaded you have to first log in and provide your full prn user id. bigger-fleet app will then use your login provider to autenticate you.

```
$ bigger-fleet login prn::pantahub.com:auth/user1
Please open the following URL in your provider and follow the instructions.

 Open: https://auth.pantahub.com/login?token=asd123asd9asdf123rj124123912319123asd&callback=.....

... Waiting ... (Abort attempt with Ctrl-C)
... Waiting ... (Abort attempt with Ctrl-C)
... Waiting ... (Abort attempt with Ctrl-C)
...
Authenticated as 'auth/user1.
```

# Managing devices

bigger-fleet does not directly communicate with devices. It indirectly manages a users fleet by accessing the users device registry and the Device Trails provider on behalf of the user.

The user can make fine grained decision which devices the bigger-fleet app
can see and manage.

For that all pantahub cloud certified auth providers offer a resource selector for the auth consent screen that in our case enables the user to make a decision which devices bigger-fleet can see.

For that the device registry has to implement the standard pantahub-auth-resource-select API and must be properly registered with the PANTAHUB.xyz registry.

## Import Devices into your Fleets (from Device Registry)

To entrust management of some or all of your devices you use the add-devices command.

```
$ bigger-fleet add-devices
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

You can now use bigger-fleet to organize and manage these. Try auto-fleet to get a quick start...
```

## Listing devices

All devices just added are not part of a fleet yet (-) as we have neither created a fleet, nor assigned devices to it.

```
$ bigger-fleet devices
DEVICEID-1@cisco.com    "bathroom-wifi"        OK  -
DEVICEID-2@cisco.com    "shower-wifi"          OK  -
DEVICEID-3@cisco.com    "gardenleft-wifi"      OK  -
DEVICEID-4@cisco.com    "gardenright-wifi"     OK  -
DEVICEID-5@cisco.com    "gardenpool-wifi"      OK  -
DEVICEID-7@cisco.com    "living-wifi"          OK  -
DEVICEID-8@cisco.com    "attic-wifi"           OK  -
DEVICEID-9@pantahub.com    "devbox1-wifi"         OK  -
DEVICEID-A@pantahub.com    "devbox2-wifi"         OK  -
DEVICEID-B@pantahub.com    "devbox3-wifi"         OK  -
DEVICEID-C@pantahub.com    "devbox4-wifi"         OK  -
```

## Device Attributes

Device attributes is meta data associated with devices useful for slice and dicing your device fleet.

The dataset of all device attributes is coming from three sources:

1. system-info reported by the device itself. These include info about hardware specs, software stack, but can also include further meta data contributed by platforms and apps. 
2. user provided meta info from the device registry
3. basic fleet meta set by bigger-fleet for processing purpose 


First you can look at attributes per device:

```
$ bigger-fleet attributes DEVICEID-1@cisco.com
Manufacturer: Cisco
Model: 121XPs
CPU: MIPS Lantiq 1234
Mem: 64M
Flash: 8M NOR
Timezone: Asia/Hong Kong
Country: China
State: Hong Kong
Region: Hong Kong 1 (GOLD COAST)
Device-Name: bathroom-wifi
Device-Location: lat:43.54123,lon:-76.123123
Device-Tags: beta-device, user-tag2
Fleet-Tags:  fleet-tag1, fleet-tag2
```

The system info is reported by device. 

Device- entries like Location come from the device registry and this meta data can be set there by the owner of a device.

Fleet- entries are the ones that bigger-fleet keeps in it's database for a device. Users of bigger-fleet can edit this using the bigger-fleet API.

## Fleet Tags

Similar the fleet app itself allows the fleet manager to use tags to mark devices for special filtering purposes.

For instance, the manager might decide that a certain machine is part of a canary set as he has that device well instrumental for debugging and use the fleet tag "canary" to do so.

```
$ bigger-fleet tags add /co/cisco/121XPs/DEVICEID-5@cisco.com canary1
Tag "canary" added to DEVICEID-5@cisco.com.
```

Similarly the admin might make a conservative tag for devices at mission critical locations such as wifi on a trading floor.

```
$ bigger-fleet tags add /location/trading-floor/* "conservative"
Tag "conservative" added to DEVICEID-5@cisco.com.
```

Also you can remove tags like:

```
$ bigger-fleet tags remove /location/trading-floor/* conservative
Removed tag "conservative" from 1 device.
```

## Grouping your Devices

For the sake of introspection, monitoring and grouping we offer a way express hierarchical grouping through attribute matching language that can be navigated by the client.

Those groupings are not exclusive - hence you can find the same device at different places in the tree.

```
# bigger-fleet addgroup <parentgroup> <subgroup> <attribute-match>
$ bigger-fleet addgroup / /co/cisco/121XPs \
	Manufacturer=="Cisco" \
	Model=="121XPs"

Adding Group "/co/cisco/121XPs" (refining /).
Currently 3 out of 8 matches..

$ bigger-fleet ls /
/co/cisco/121XPs/DEVICEID-5@cisco.com TAG1,TAG2,TAG3
/co/cisco/121XPs/DEVICEID-2@cisco.com TAG2,TAG4
/co/cisco/121XPs/DEVICEID-3@cisco.com -
Found 3 devices
```

# Rollouts

Changes to the fleet are modeled in so called ```Rollouts```.

A ```Rollout``` describes how changes get dissiminated across the fleet in multiple steps. 

Rollouts allow you to model a phased rollout including experiments in the field.

A Rollout can be in DRAFT, ACTIVE, DONE, ERROR state and are modelled as a multi step action plan

While Rollout is in DRAFT state you can add steps to it. For all other steps you cannot change
the definition.

Each step:
* selects a subset of devices
* has time window in which the change should be rolled out evenly distributed across the selected devices
* has a timeout that specifies how long to wait for devices reporting back at the end of the time window before making the success decision
* has a success criteria configured that defines the minimum ratio of devices that need to report results and what the success rate of those need to be.

For instance you specify `success=80,99.5` means that a rollout continues to the next step only if 80% of the devices have reported their results and 99.5% of those were successful.


## Navigating Your Rollouts

The ```rollouts``` subcommand gives a summary of the state of all rollouts.


```
$ bigger-fleet rollouts
ROLLOUT1 DRAFT (Step 0/5, ETA: 5 days)
ROLLOUT2 ACTIVE (Step: 2/5, Running since: 1 day; ETA: 3 days)
ROLLOUT3 DONE (Succeeded @ Aug 12, 2016 - 21:12:22 UTC with 95,99.7%)
ROLLOUT4 ERROR (Failed @ Step: 3/5, ERROR: 80,80%)
```

The above shows you a rollout in each state:

The DRAFT ROLLOUT1 has not been activated yet; hence it is at step 0 of 5. you can add or remove steps at this stage still. To activate the rollout you switch the status: field to ACTIVE.

The ACTIVE ROLLOUT2 is at step 2 out of 5 and has been running since 1 day. If all goes smooth this rollout will latest be finished in 3 days from now.

The DONE ROLLOUT3 is done. So far 95% devices reported 99.7% success. Note that those failed will be retried.

The ERROR ROLLOUT4 failed to succeed step 3. After 80% of the devices reporting back the success rate is only 80%, indicating a serious problem that needs investigation.

## Creating a Rollout

As explained above a Rollout basically is a set of changes to be applied to the trails of selected devices following a schedule.

To create a rollout, use the rollouts add subcommand, give it a name and select the base set of devices this rollout will target.

```
$ bigger-fleet rollouts add ROLLOUT5 \
    change="set kernel=prn::pantahub.com:objects:/aq921391231239awd" \
	message="update kernel across device fleet"
	devices=/
	time="5d" \
	timeout="12h" \
	success="85%,99%"
Created Rollout "ROLLOUT5". Currently 23 devices in rollout.
```
The above creates a rollout that applies a new kernel to all devices (e.g. devices=/) scheduled over course of 5 days with success criteria being 85% of devices reporting 99% success.

## Adding steps

To have more fine grained control and ensure that the big rollout does not proceed without you having assurance about the quality of your release, you can add a new step for your canary devices like:

```
$ bigger-fleet rollouts ROLLOUT5 stepadd \
     devices=/specialsets/canary \
	 time="4h" \
	 timeout="1h" \
	 success="100%,100%"
Added step for '/specialsets/canary'
```

The above will add a step that will deploy the change of the special set of canary devices evenly over the next 4h and give devices a time of 1h to pick this up and report back. The rollout will only continue if all canary devices have reported success (100%,100%).

If no further step are added, there will always be an implicit last step that will do an even rollout across all devices of the rollout set that were not in any previous step.

```
$ bigger-fleet rollouts ROLLOUT5
Status: DRAFT
1. '/specialsets/canary' - status: DRAFT; config: time 4h, timeout 1h, min-reports: 100%, min-success 100%
FINAL. ALL - status: DRAFT; config: time 4h, timeout 1h, min-reports: 85%, min-success: 99%
```

## Deleting steps

You might want to remove a step or a complete rollout if its in DRAFT state.

```
$ bigger-fleet rollout ROLLOUT5 step-delete 1
Deleted step 1 from 'ROLLOUT5'.
```

## Deleting rollouts

You might want to discard a rollout as a whole that is in DRAFT state.

```
$ bigger-fleet rollout-delete ROLLOUT5
Deleted rollout DRAFT 'ROLLOUT5'.
```

## Activate a rollout

Once you are finished drafting and modeling your rollout you want to activate it using the special activate command for rollout:

```
$ bigger-fleet rollouts ROLLOUT5 activate
Activating rollout ROLLOUT5; Scheduled.
```

Once activated no further steps can be added or removed while the rollout is active.

## Monitoring the rollout

You can look at how your rollout proceeds using the rollouts command again:

```
$ bigger-fleet rollouts ROLLOUT5
Status: ACTIVE
1. '/specialsets/canary' - status: INPROGRESS, ETA 3h, progress: 50%, success: 100%
2. ALL - status: QUEUE
```

# Errors and Resolutions (XXX: TODO)

Fleet management of a distributed fleet of devices are tricky in the sense that
there is far more likely temporary or permanent device failues to be seen due
to nature of devices of the same fleet living in different types of environments.

Heat, outages, etc. are example of external environment variables that can
influence how device updates succeed or not.

Also devices might be of different type of hardware and hence might have different
error conditions for software running on them.

Also devices might often be offline for an unknown amount of time as the user might
decide to turn it off for month's, before booting the device up again. Being
able to have those devices still get the changes from fleet management, while
not blocking the whole fleet is crucial.

To allow fleet management to be effective, this data needs to be available for
decision making during the software rollout process. Further, an optimistic approach
needs taking, while preserving the ability to manaually hand hold that were left behind
to catch up withe the rollouts of the devicefleet.

XXXX: needs drafting

## Noticing Errors

```
bigger-fleet failures ROLLOUT#2

DEVICE-B@pantahub.com@2  "update App to X"
Error Message: "app checksum mismatch - download corrupted."

DEVICE-C@pantahub.com@2  "update App to X"
Error Message: "starting new app failed - segementation fault."
Core: "object-id-to-coredump"

```

## Resolving Errors - Retry

Errors can sometimes be resolved by retrying at a later time. Like in the above case above the CDN assigned for that device temporarily was serving corrupted content.

```
bigger-fleet retry DEVICE-B@pantahub.com@2
Thanks. Scheduled a retry
```

## Resolving Errors - Backout

XXX

## Resolving Errors - Reroute

But regularly in a world of devices there might be a need to implement some workarounds or migration code that gets run on certain devices within the fleet to allow updating from version X of app to Y.

When identifying for instance that its infeasible to upgrade to app version Y on certain devices because of two low memory during the data migration, it could be useful to go a two step upgrade approach for those devices while not making all other devices go through the extra hop as well.

In our example, lets assume that our kernel 4.1 does not boot anymore without updating some firmware first. Hence we make a special kernel that does just the firmware upgrade and inject that kernel as a reroute for the failed devices in our fleet.

```
bigger-fleet reroute ROLLOUT#2 ALLFAIL
Rerouting ALLFAIL.
```
This will temporarily move the failed devices into a sub-fleet of my-kernel-fleet that holds all those devices that go on the reroute detour.


# Requirements - NOTES

User Activities to walk through:

* Getting the CLI App [OK]
* Logging In [OK]
* Import Devices into your Fleets (from Device Registry) [OK]
* Device Attributes [OKish]
	* read-only attributes (reported system info) [OK]
	* special random systemid for randomized scheduling heurisitics [TODO]
	* user attributes (aka tags) [OK]
* Organize Devices into Fleets [OK]
	* through attribute matching
* do not worry about duplicate appearances at this ping
* Rollouts [INPROGRESS]
	* basics [OK]
* Draft a Rollout
	* create
	* add steps
		* define schedule
			* only fire for targets matched through attribute expression
		* the changes to apply
			* changes conditional by attribute (redundant concept. could be done in separate rollout; just changes would be fine i think)
		* the success criteria thresholds (reported, success rate)
* Activating a rollout
* Monitoring a rollout
* Aborting/Pausing a rollout
* rolling back
    * auto
    * manual
* basic monitoring (fleet load, metrics)
