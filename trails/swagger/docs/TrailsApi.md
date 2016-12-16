# \TrailsApi

All URIs are relative to *https://api.pantahub.com/v1/*

Method | HTTP request | Description
------------- | ------------- | -------------
[**TrailsGet**](TrailsApi.md#TrailsGet) | **Get** /trails | Get Trails visible in calling context
[**TrailsPost**](TrailsApi.md#TrailsPost) | **Post** /trails | Create new Trailing for device


# **TrailsGet**
> []Trail TrailsGet($start, $maxitems)

Get Trails visible in calling context

Get a list of trails that are visible to the user/principle/roles associated with the calling context. Users usually see trails for all their devices, but devices only see their very own. 


### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **start** | **float32**| Item top start this page | [optional] 
 **maxitems** | **float32**| Max Items to retrieve (default \&quot;all\&quot;) | [optional] 

### Return type

[**[]Trail**](Trail.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **TrailsPost**
> []Trail TrailsPost($factoryState, $maxitems)

Create new Trailing for device

Create a new Trail for the calling device. The Trail will have the same ID as the calling device. 


### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **factoryState** | [**State**](State.md)| Factory state json to seed during trail creation | 
 **maxitems** | **float32**| Max Items to retrieve (default \&quot;all\&quot;) | [optional] 

### Return type

[**[]Trail**](Trail.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

