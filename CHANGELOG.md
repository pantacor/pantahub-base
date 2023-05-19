
<a name="042-rev1"></a>
## [042-rev1](https://gitlab.com/pantacor/pantahub-base/compare/042...042-rev1) (2023-05-19)

### Fix

* **ci:** correct gitlab variables when building tags
* **exports:** create tarball using the step modified date instead of now


<a name="042"></a>
## [042](https://gitlab.com/pantacor/pantahub-base/compare/041...042) (2023-05-18)

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
## [041](https://gitlab.com/pantacor/pantahub-base/compare/040-rev1...041) (2023-05-05)

### Fix

* **auth:** remove challenge on user password change to activate account


<a name="040-rev1"></a>
## [040-rev1](https://gitlab.com/pantacor/pantahub-base/compare/040...040-rev1) (2023-03-13)

### Fix

* **trails:** add findoptions to find one trails revision


<a name="040"></a>
## [040](https://gitlab.com/pantacor/pantahub-base/compare/039...040) (2023-03-13)

### Fix

* **trails:** get trails revision correct authorization logic


<a name="039"></a>
## [039](https://gitlab.com/pantacor/pantahub-base/compare/038...039) (2023-03-10)

### Feature

* **trails:** revision step fileds projection as in steps list


<a name="038"></a>
## [038](https://gitlab.com/pantacor/pantahub-base/compare/037...038) (2023-03-03)

### Feature

* **trails:** add filters and fields selection to steps list endpoint


<a name="037"></a>
## [037](https://gitlab.com/pantacor/pantahub-base/compare/036...037) (2023-01-16)

### Fix

* **tracing:** don't read uber-trace-id if traceparent is already set


<a name="036"></a>
## [036](https://gitlab.com/pantacor/pantahub-base/compare/035...036) (2023-01-11)

### Fix

* **tracing:** correct uber-trace-id header name


<a name="035"></a>
## [035](https://gitlab.com/pantacor/pantahub-base/compare/034...035) (2023-01-10)

### Feat

* **tracing:** read uber-trace-id inserted by jaeger on ingress-nginx


<a name="034"></a>
## [034](https://gitlab.com/pantacor/pantahub-base/compare/033...034) (2023-01-09)

### Feat

* **trace:** add trace span for writejson and response write


<a name="033"></a>
## [033](https://gitlab.com/pantacor/pantahub-base/compare/032...033) (2023-01-04)


<a name="032"></a>
## [032](https://gitlab.com/pantacor/pantahub-base/compare/031...032) (2022-09-13)


<a name="031"></a>
## [031](https://gitlab.com/pantacor/pantahub-base/compare/030...031) (2022-09-13)

### Feat

* **subscriptions:** add stripe subscriptions to whitelisted type of plans

### Fix

* **logs:** show elastic search error inside the incidents logs

### Perf

* **logs:** use elastic search filter instead must for logs search


<a name="030"></a>
## [030](https://gitlab.com/pantacor/pantahub-base/compare/029...030) (2022-07-04)

### Fix

* **subscriptions:** correct Subscription History notation


<a name="029"></a>
## [029](https://gitlab.com/pantacor/pantahub-base/compare/028...029) (2022-04-08)

### Feature

* support elasticsearch 7.17

### Fix

* **mails:** attach only necessary images


<a name="028"></a>
## [028](https://gitlab.com/pantacor/pantahub-base/compare/027...028) (2022-01-24)

### Feature

* **emails:** changes to assests, style and layout

### Fix

* **devices:** device tokens need to quote the dots on DefaultUserMeta


<a name="027"></a>
## [027](https://gitlab.com/pantacor/pantahub-base/compare/026...027) (2022-01-11)

### Feat

* **storage:** support dynamic s3 region selection via k8s node roles


<a name="026"></a>
## [026](https://gitlab.com/pantacor/pantahub-base/compare/025-r01...026) (2021-10-28)


<a name="025-r01"></a>
## [025-r01](https://gitlab.com/pantacor/pantahub-base/compare/025...025-r01) (2021-09-15)


<a name="025"></a>
## [025](https://gitlab.com/pantacor/pantahub-base/compare/024-r02...025) (2021-07-14)

### Feature

* User profile meta data


<a name="024-r02"></a>
## [024-r02](https://gitlab.com/pantacor/pantahub-base/compare/024-r01...024-r02) (2021-06-29)


<a name="024-r01"></a>
## [024-r01](https://gitlab.com/pantacor/pantahub-base/compare/024...024-r01) (2021-06-25)


<a name="024"></a>
## [024](https://gitlab.com/pantacor/pantahub-base/compare/023...024) (2021-06-02)


<a name="023"></a>
## [023](https://gitlab.com/pantacor/pantahub-base/compare/022...023) (2021-02-01)

### Resterror

* log incidents as RError struct to fluentd using tag 'com.pantahub-base.incidents'


<a name="022"></a>
## [022](https://gitlab.com/pantacor/pantahub-base/compare/021-rv4...022) (2021-01-11)

### Feature

* Add logo and name to thirdparty applications
* split verfication email in two emails: welcome and activation


<a name="021-rv4"></a>
## [021-rv4](https://gitlab.com/pantacor/pantahub-base/compare/021-rv3...021-rv4) (2020-12-15)


<a name="021-rv3"></a>
## [021-rv3](https://gitlab.com/pantacor/pantahub-base/compare/021-rv2...021-rv3) (2020-12-15)


<a name="021-rv2"></a>
## [021-rv2](https://gitlab.com/pantacor/pantahub-base/compare/021-rv1...021-rv2) (2020-12-11)


<a name="021-rv1"></a>
## [021-rv1](https://gitlab.com/pantacor/pantahub-base/compare/021...021-rv1) (2020-12-11)


<a name="021"></a>
## [021](https://gitlab.com/pantacor/pantahub-base/compare/020...021) (2020-10-15)


<a name="020"></a>
## [020](https://gitlab.com/pantacor/pantahub-base/compare/019-rv1...020) (2020-10-08)


<a name="019-rv1"></a>
## [019-rv1](https://gitlab.com/pantacor/pantahub-base/compare/019...019-rv1) (2020-10-08)


<a name="019"></a>
## [019](https://gitlab.com/pantacor/pantahub-base/compare/018...019) (2020-10-01)


<a name="018"></a>
## [018](https://gitlab.com/pantacor/pantahub-base/compare/017...018) (2020-09-23)


<a name="017"></a>
## [017](https://gitlab.com/pantacor/pantahub-base/compare/016...017) (2020-09-14)


<a name="016"></a>
## [016](https://gitlab.com/pantacor/pantahub-base/compare/015...016) (2020-08-02)


<a name="015"></a>
## [015](https://gitlab.com/pantacor/pantahub-base/compare/014...015) (2020-07-03)


<a name="014"></a>
## [014](https://gitlab.com/pantacor/pantahub-base/compare/013...014) (2020-07-01)


<a name="013"></a>
## [013](https://gitlab.com/pantacor/pantahub-base/compare/012...013) (2020-06-03)

### Fix

* GET /profiles was not returning the owner(requesting user) nick as the user have  no public devices


<a name="012"></a>
## [012](https://gitlab.com/pantacor/pantahub-base/compare/011...012) (2020-04-07)


<a name="011"></a>
## [011](https://gitlab.com/pantacor/pantahub-base/compare/010-rc2...011) (2020-03-30)


<a name="010-rc2"></a>
## [010-rc2](https://gitlab.com/pantacor/pantahub-base/compare/010-rc1...010-rc2) (2020-02-06)


<a name="010-rc1"></a>
## [010-rc1](https://gitlab.com/pantacor/pantahub-base/compare/009...010-rc1) (2020-01-14)


<a name="009"></a>
## [009](https://gitlab.com/pantacor/pantahub-base/compare/009-rc1...009) (2019-08-15)


<a name="009-rc1"></a>
## [009-rc1](https://gitlab.com/pantacor/pantahub-base/compare/007...009-rc1) (2019-08-15)


<a name="007"></a>
## [007](https://gitlab.com/pantacor/pantahub-base/compare/006...007) (2019-06-27)


<a name="006"></a>
## [006](https://gitlab.com/pantacor/pantahub-base/compare/005...006) (2019-04-15)

### Devices

* restrict secrets and metadata access to authorized accounts

### Document

* # Service authorization with access tokens (aka oauth2'ish authorization flow)


<a name="005"></a>
## [005](https://gitlab.com/pantacor/pantahub-base/compare/005-rc1...005) (2019-01-11)


<a name="005-rc1"></a>
## [005-rc1](https://gitlab.com/pantacor/pantahub-base/compare/004...005-rc1) (2018-10-26)


<a name="004"></a>
## [004](https://gitlab.com/pantacor/pantahub-base/compare/004-rc2...004) (2018-05-31)

### Devices

* use timemodified and timecreated consistently on bson/mgo side
* fix missing time-modified updates for write operations


<a name="004-rc2"></a>
## [004-rc2](https://gitlab.com/pantacor/pantahub-base/compare/004-rc1...004-rc2) (2018-05-28)


<a name="004-rc1"></a>
## [004-rc1](https://gitlab.com/pantacor/pantahub-base/compare/002.1...004-rc1) (2018-05-28)

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
## [002.1](https://gitlab.com/pantacor/pantahub-base/compare/002...002.1) (2017-12-03)


<a name="002"></a>
## [002](https://gitlab.com/pantacor/pantahub-base/compare/001...002) (2017-10-26)


<a name="001"></a>
## 001 (2017-06-19)

