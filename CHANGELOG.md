
<a name="050"></a>
## [050](https://gitlab.com/pantacor/pantahub-base/compare/049...050)

> 2026-07-21

### Fix

* support HTTP Range requests on S3-backed object downloads
* **trails:** name the object that cannot be resolved in a state


<a name="049"></a>
## [049](https://gitlab.com/pantacor/pantahub-base/compare/048...049)

> 2026-07-14

### Feat

* support HTTP Basic Auth on POST /auth/login
* HTTP Basic Auth → Bearer JWT translation middleware

### Feature

* add MetaModified field to Device struct
* device export should buffer and calculate the sha
* add devicemetamodified
* add /auth/token/refresh for service-issued tokens

### Fix

* update golang-petname dependency
* require service identity to refresh service-issued tokens
* bson quote and unquote does walk the whole json
* subscriptions is admin, allow token with roles or type admin
* reset mark_public_processed when device ispublic changes
* add cronjobs into the docker-compose
* use a create device nick function
* create a retry and change the nick in case of colision
* make the device nick less prone to collitions
* set defaullt for authorize token to 5 days
* token refresh use the jwt authorize timeout
* **logs:** drop-and-log permanent _bulk failures so devices don't stall
* **logs:** detect elasticsearch _bulk per-item errors so large batches are not silently dropped

### Refactor

* bson tool have less point with the string to replace


<a name="048"></a>
## [048](https://gitlab.com/pantacor/pantahub-base/compare/047...048)

> 2026-04-01

### Feat

* add retries field to StepProgress model

### Feature

* the JWT timeout of pending ovmode devices is configurable via environment variables
* device with pending ovmode we should send a short live token
* add polling to pkce oauth method
* add support to pkce oauth authorization flow for cli

### Fix

* add default-user-meta to the patch of device tokens
* make the refresh handle safe by catching the panic
* correct the timeouts for the pkce token and the authorization token
* docker kafka and kafka stability
* validating response before the error
* db field for default-user-meta is defaultusermeta
* Add orig_iat to JWT claims
* patches only update necessary data
* make device-meta and user-meta patch atomic
* flatten values to generate atomic update of user-meta or device-meta
* make sure the keys of the device-meta and user-meta as bsonquoted
* reading consistency for devices
* make sure phs start correctly when docker compose up -d base
* **devices:** prevent double response in delete device endpoint by refactoring MarkDeviceAsGarbage error handling


<a name="047"></a>
## [047](https://gitlab.com/pantacor/pantahub-base/compare/046...047)

> 2026-02-09

### Feature

* **devices:** enforce quota on device claiming
* **devices:** enforce quota on device claiming

### Fix

* correct entraid UserPrincipalName instead of email
* trails progress logs had incorrect json key


<a name="046"></a>
## [046](https://gitlab.com/pantacor/pantahub-base/compare/045...046)

> 2025-12-04

### Feature

* add endpoint for patch and get device tokens
* add support to pkce oauth authorization flow for cli
* add error when device is already created
* **auth:** add variables to disable login and password reset
* **auth:** disable signup and signin using an environment variable
* **auth:** allow personal token login with account username
* **device_tokens:** add tls onboarding for devices
* **devices:** ownership validation should answer done if it was already done
* **devices:** if device is not found in all the devices actions return 404 not found
* **exports:** exports device should accept the owner name or owner_id
* **oauth:** add support to entraid
* **s3:** add multiprovider download
* **steps:** add endpoint to make a revision as WONTGO
* **tokens:** add token managment to create personal tokens
* **tools:** add kibana to the docker-compose for development
* **trails:** add content-length to response of canonical json enconder

### Fix

* make sure _id to find apps only if it can get the object id
* allow auth_code in authorization to use to search pkce
* use default timeout of 30 for all elastic related request
* **api:** allow always the trace-id headers and content-length
* **auth:** token created from personal tokens can't be refresh
* **auth:** disable password login by using the PANTAHUB_DISABLE_EMAIL_PASSWORD_LOGIN variable
* **auth:** oauth2 logs and error handling
* **auth:** error login as application to exchange token
* **device:** ownership validation with certificates should return better errors
* **device-meta:** use a variable for the parsingErrorKey
* **device-meta:** use a variable for the parsingErrorKey
* **device-meta:** when patching device meta, if there is an error parsing, update the device meta with an error
* **devices:** on devices list process correctly error and remove hide the meta by default
* **devices:** device-meta path should search for device update meta and them save new metas
* **fluentd:** log into fluentd request and response payload
* **logs:** pagination can not be more than 500
* **logs:** use context without cancel for logs post
* **logs:** timeout posting to elastic
* **logs:** don't return the new entries on the logs after saving
* **mongo:** always quote $ and .
* **objects:** check for correct error handling of linked objects on creation
* **objects:** linked objects should search for initial object and then see if public
* **objects:** objects should be move only after correctly upload and the exists should validate the objects existance
* **ovmode:** ownership validation should suport certificates chain for validation
* **s3:** rename after upload should be done to objectfinalname
* **s3:** make sure that the download url is created with the objects id
* **s3:** update aws sdk and s3 package
* **steps:** new steps with rev -1 should be created correctly
* **swagger:** add /auth/login endpoint
* **trails:** add search indexes and sort index trails collection
* **xss:** sanatize inputs for html injection


<a name="045"></a>
## [045](https://gitlab.com/pantacor/pantahub-base/compare/044...045)

> 2023-09-28

### Fix

* **objects:** trails post context timeout


<a name="044"></a>
## [044](https://gitlab.com/pantacor/pantahub-base/compare/043...044)

> 2023-09-28

### Feature

* **.pvrremote:** add step get url to /trails/.pvrremote
* **.pvrremote:** add step get url to .pvrremote
* **pvrremote:** add get step revision url to response

### Fix

* **callbacks:** do not fail if state values aren't objects
* **mail:** add mailgun url from environment
* **objects:** SaveObject should use parent context to save instead of 5 minutes timeout
* **trails:** create a children context for database actions
* **trails:** create a children context for database actions

### Refactor

* **email:** update mailgun library and go version
* **email:** add messeges to trace errors with mailgun
* **email:** add messeges to trace errors with mailgun


<a name="043"></a>
## [043](https://gitlab.com/pantacor/pantahub-base/compare/042-rev1...043)

> 2023-05-24

### Fix

* **exports:** get a reproducible sha sum when creating the same revision export
* **exports:** use same modtime for json and object files


<a name="042-rev1"></a>
## [042-rev1](https://gitlab.com/pantacor/pantahub-base/compare/042...042-rev1)

> 2023-05-19

### Fix

* **ci:** correct gitlab variables when building tags
* **exports:** create tarball using the step modified date instead of now


<a name="042"></a>
## [042](https://gitlab.com/pantacor/pantahub-base/compare/041...042)

> 2023-05-18

### Feature

* **dev tools:** add full docker configuration to debug and test kafka pipelines
* **exports:** create endpoint to export devices tarball

### Fix

* **ci:** update gitlab variables
* **docs:** solve documentation models reference
* **exports:** add correct header to download file

### Refactor

* **auth:** move auth model to proper authmodels package
* **auth:** allow jwt authentication to run in all aps


<a name="041"></a>
## [041](https://gitlab.com/pantacor/pantahub-base/compare/040-rev1...041)

> 2023-05-05

### Fix

* **auth:** remove challenge on user password change to activate account


<a name="040-rev1"></a>
## [040-rev1](https://gitlab.com/pantacor/pantahub-base/compare/040...040-rev1)

> 2023-03-13

### Fix

* **trails:** add findoptions to find one trails revision


<a name="040"></a>
## [040](https://gitlab.com/pantacor/pantahub-base/compare/039...040)

> 2023-03-13

### Fix

* **trails:** get trails revision correct authorization logic


<a name="039"></a>
## [039](https://gitlab.com/pantacor/pantahub-base/compare/038...039)

> 2023-03-10

### Feature

* **trails:** revision step fileds projection as in steps list


<a name="038"></a>
## [038](https://gitlab.com/pantacor/pantahub-base/compare/037...038)

> 2023-03-03

### Feature

* **trails:** add filters and fields selection to steps list endpoint


<a name="037"></a>
## [037](https://gitlab.com/pantacor/pantahub-base/compare/036...037)

> 2023-01-16

### Fix

* **tracing:** don't read uber-trace-id if traceparent is already set


<a name="036"></a>
## [036](https://gitlab.com/pantacor/pantahub-base/compare/035...036)

> 2023-01-11

### Fix

* **tracing:** correct uber-trace-id header name


<a name="035"></a>
## [035](https://gitlab.com/pantacor/pantahub-base/compare/034...035)

> 2023-01-10

### Feat

* **tracing:** read uber-trace-id inserted by jaeger on ingress-nginx


<a name="034"></a>
## [034](https://gitlab.com/pantacor/pantahub-base/compare/033...034)

> 2023-01-09

### Feat

* **trace:** add trace span for writejson and response write


<a name="033"></a>
## [033](https://gitlab.com/pantacor/pantahub-base/compare/032...033)

> 2023-01-04


<a name="032"></a>
## [032](https://gitlab.com/pantacor/pantahub-base/compare/031...032)

> 2022-09-13


<a name="031"></a>
## [031](https://gitlab.com/pantacor/pantahub-base/compare/030...031)

> 2022-09-13

### Feat

* **subscriptions:** add stripe subscriptions to whitelisted type of plans

### Fix

* **logs:** show elastic search error inside the incidents logs

### Perf

* **logs:** use elastic search filter instead must for logs search


<a name="030"></a>
## [030](https://gitlab.com/pantacor/pantahub-base/compare/029...030)

> 2022-07-04

### Fix

* **subscriptions:** correct Subscription History notation


<a name="029"></a>
## [029](https://gitlab.com/pantacor/pantahub-base/compare/028...029)

> 2022-04-08

### Feature

* support elasticsearch 7.17

### Fix

* **mails:** attach only necessary images


<a name="028"></a>
## [028](https://gitlab.com/pantacor/pantahub-base/compare/027...028)

> 2022-01-24

### Feature

* **emails:** changes to assests, style and layout

### Fix

* **devices:** device tokens need to quote the dots on DefaultUserMeta


<a name="027"></a>
## [027](https://gitlab.com/pantacor/pantahub-base/compare/026...027)

> 2022-01-11

### Feat

* **storage:** support dynamic s3 region selection via k8s node roles


<a name="026"></a>
## [026](https://gitlab.com/pantacor/pantahub-base/compare/025-r01...026)

> 2021-10-28


<a name="025-r01"></a>
## [025-r01](https://gitlab.com/pantacor/pantahub-base/compare/025...025-r01)

> 2021-09-15


<a name="025"></a>
## [025](https://gitlab.com/pantacor/pantahub-base/compare/024-r02...025)

> 2021-07-14

### Feature

* User profile meta data


<a name="024-r02"></a>
## [024-r02](https://gitlab.com/pantacor/pantahub-base/compare/024-r01...024-r02)

> 2021-06-29


<a name="024-r01"></a>
## [024-r01](https://gitlab.com/pantacor/pantahub-base/compare/024...024-r01)

> 2021-06-25


<a name="024"></a>
## [024](https://gitlab.com/pantacor/pantahub-base/compare/023...024)

> 2021-06-02


<a name="023"></a>
## [023](https://gitlab.com/pantacor/pantahub-base/compare/022...023)

> 2021-02-01

### Resterror

* log incidents as RError struct to fluentd using tag 'com.pantahub-base.incidents'


<a name="022"></a>
## [022](https://gitlab.com/pantacor/pantahub-base/compare/021-rv4...022)

> 2021-01-11

### Feature

* Add logo and name to thirdparty applications
* split verfication email in two emails: welcome and activation


<a name="021-rv4"></a>
## [021-rv4](https://gitlab.com/pantacor/pantahub-base/compare/021-rv3...021-rv4)

> 2020-12-15


<a name="021-rv3"></a>
## [021-rv3](https://gitlab.com/pantacor/pantahub-base/compare/021-rv2...021-rv3)

> 2020-12-15


<a name="021-rv2"></a>
## [021-rv2](https://gitlab.com/pantacor/pantahub-base/compare/021-rv1...021-rv2)

> 2020-12-11


<a name="021-rv1"></a>
## [021-rv1](https://gitlab.com/pantacor/pantahub-base/compare/021...021-rv1)

> 2020-12-11


<a name="021"></a>
## [021](https://gitlab.com/pantacor/pantahub-base/compare/020...021)

> 2020-10-15


<a name="020"></a>
## [020](https://gitlab.com/pantacor/pantahub-base/compare/019-rv1...020)

> 2020-10-08


<a name="019-rv1"></a>
## [019-rv1](https://gitlab.com/pantacor/pantahub-base/compare/019...019-rv1)

> 2020-10-08


<a name="019"></a>
## [019](https://gitlab.com/pantacor/pantahub-base/compare/018...019)

> 2020-10-01


<a name="018"></a>
## [018](https://gitlab.com/pantacor/pantahub-base/compare/017...018)

> 2020-09-23


<a name="017"></a>
## [017](https://gitlab.com/pantacor/pantahub-base/compare/016...017)

> 2020-09-14


<a name="016"></a>
## [016](https://gitlab.com/pantacor/pantahub-base/compare/015...016)

> 2020-08-02


<a name="015"></a>
## [015](https://gitlab.com/pantacor/pantahub-base/compare/014...015)

> 2020-07-03


<a name="014"></a>
## [014](https://gitlab.com/pantacor/pantahub-base/compare/013...014)

> 2020-07-01


<a name="013"></a>
## [013](https://gitlab.com/pantacor/pantahub-base/compare/012...013)

> 2020-06-03

### Fix

* GET /profiles was not returning the owner(requesting user) nick as the user have  no public devices


<a name="012"></a>
## [012](https://gitlab.com/pantacor/pantahub-base/compare/011...012)

> 2020-04-07


<a name="011"></a>
## [011](https://gitlab.com/pantacor/pantahub-base/compare/010-rc2...011)

> 2020-03-30


<a name="010-rc2"></a>
## [010-rc2](https://gitlab.com/pantacor/pantahub-base/compare/010-rc1...010-rc2)

> 2020-02-06


<a name="010-rc1"></a>
## [010-rc1](https://gitlab.com/pantacor/pantahub-base/compare/009-rc1...010-rc1)

> 2020-01-14


<a name="009-rc1"></a>
## [009-rc1](https://gitlab.com/pantacor/pantahub-base/compare/009...009-rc1)

> 2019-08-15


<a name="009"></a>
## [009](https://gitlab.com/pantacor/pantahub-base/compare/007...009)

> 2019-08-15


<a name="007"></a>
## [007](https://gitlab.com/pantacor/pantahub-base/compare/006...007)

> 2019-06-27


<a name="006"></a>
## [006](https://gitlab.com/pantacor/pantahub-base/compare/005...006)

> 2019-04-15

### Devices

* restrict secrets and metadata access to authorized accounts

### Document

* # Service authorization with access tokens (aka oauth2'ish authorization flow)


<a name="005"></a>
## [005](https://gitlab.com/pantacor/pantahub-base/compare/005-rc1...005)

> 2019-01-11


<a name="005-rc1"></a>
## [005-rc1](https://gitlab.com/pantacor/pantahub-base/compare/004...005-rc1)

> 2018-10-26


<a name="004"></a>
## [004](https://gitlab.com/pantacor/pantahub-base/compare/004-rc2...004)

> 2018-05-31

### Devices

* use timemodified and timecreated consistently on bson/mgo side
* fix missing time-modified updates for write operations


<a name="004-rc2"></a>
## [004-rc2](https://gitlab.com/pantacor/pantahub-base/compare/004-rc1...004-rc2)

> 2018-05-28


<a name="004-rc1"></a>
## [004-rc1](https://gitlab.com/pantacor/pantahub-base/compare/002.1...004-rc1)

> 2018-05-28

### Accounts

* use unique IDs and Emails for default/test users

### Devices

* fix wording in error text
* add ability to PATCH device resources for nick property. updated README.md
* add support for looking up devices by ownernick/devicenick

### Dockerfile

* make builder alpine to get proper libc in golang binary

### Logs

* use value instaed of 'after' pointer when restricting search by time
* add support for 'after' to GET /logs endpoint

### Trails

* fix typo in error message
* update LastTouch on handle_posttrail to match LastInSync

### Utils

* set default env for ELASTIC_URL to localhost


<a name="002.1"></a>
## [002.1](https://gitlab.com/pantacor/pantahub-base/compare/002...002.1)

> 2017-12-03


<a name="002"></a>
## [002](https://gitlab.com/pantacor/pantahub-base/compare/001...002)

> 2017-10-26


<a name="001"></a>
## 001

> 2017-06-19

