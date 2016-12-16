# Basics

# Resource IDs

In a federated cloud you often want to refer to resources outside your domain. To solve
this pantahub uses a urn inspired name scheme, that goes as follows:

```
  prn:<reg-id>:<provider-id>:<service-id>:/relative/resource/path
```

* reg-id: globally unique identifier of the registry that governs the provider namespace. By default this is pantahub.com.
* provider-id: federation-unique provider id referring to an entity (person, company, org, gov) that governs the service namespace
* service-id: provider unique service id

## Registry ID and Endpoint
Registry are the heart of everything. They provide the lookup to find all resources referred to by prn: as well as the public keys of the registry, provider and service providers.

To ease bootstrapping, the reg-id must be the host where the restful registry must be accessible. The URL that is expected to have the Registry API endpoint is:

```
   https://<reg-id>/api/reg/v1
```

All services hosted by the registry owner himself, wont need a provider-id, hence you will find for the default apps provided by ```pantahub.com``` resource ids like this:

```
   prn:pantahub.com::<service-id>:/resource/path
```

## Register Provider IDs

To register a provider-id you use the providers endpoint:

```
   http POST https://pantahub.xzy/reg/api/providers/ \
        id=<provider-id> \
        pubkey=<provider-pubkey> \
        name=<human name> \
        description=<some text about you> \
        email=<your-admin-email> \
        location=<www-url-for-humans> \
        password=<your-admin-password>
```

## Service ID

To register a service you have use the /services subendpoint:

```
   http POST https://pantahub.com/reg/api/providers/:provider-id/services/ \
        id=<service-id> \
        pubkey=<service-pubkey> \
        name=<service-name> \
        description=<service-description> \
        api=<prn-to-API-definition> \
        api-location="<url-to-api-endpoint-base> \
        email="administrative-email"
```

This will usually trigger an approval process with the provider. The provider
is responsible to ensure services comply to API standards and adhere the providers
governance rules.

### APIs

APIs known to the registry are available for download/inspection.

```
   http GET https://pantahub.com/reg/api/apis/
   {
      info: {
         "found": 1000
         "start": 0
         "end": 50
      }
      results: {
        [
          {
              api-id: "APINAME"
              ...
          },
          ...
       ]
   }
```

provider-id and service-id's can be reserved through the ASA API (see below). fed-ids should be rare and are only processed via email or you can use a domain that you have control over without further communication with pantahub.

## APIs and Service choices

Cloud federation only works through standardization. Standardization only
works through offer, adopt, win.

There is a minimal set of standard APIs published by pantahub that are needed
to keep things together. These are:

* Authorities, Services and APIs registry
    * API-ID: prn:pantahub.com:registry:/apis/ASAPIS/v1
    * ID: prn:pantahub.com:registry:/services/pantahub-saap-registry
* Inter Service Authorization API
    * API-ID: prn:pantahub.com:registry:/apis/device-auth/v1
    * regulating the auth token exchange format



Further **pantahub.com** publishes a reference system which includes a set of
key APIs that we believe most would want to follow:

* User and Login API - Register Users and Login
* Device Registry API - Register devices
* Licensing API - License software usage backed up by payments
* Subscription API - Subscriptions to Service and Software Offerings
* Market API - Broker Subscriptions, Licensing and Installs
* Software Publisher API - Publish Your Software in upgradable sequences
* Device Trails API - async device and config management

These are the APIs that scloud subcommands use by default.

All the above are minimalistic and we encourage each to be extended by individual
offerings, however we think its a reasonable choice to keep your APIs compatible
with these very minimalistic features.

scloud allows you to get info about APIs, authorities and services from your registry.

```
# Authorities that are allowed to approve services
$ scloud registry authorites
prn:pantahub.com:registry:/authority/pantahub.com
    pubkey: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
    location: http://authority.pantahub.com
    roles: API, service, identity,

prn:pantahub.com:registry:/authority/symantec
    pubkey: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx


```


