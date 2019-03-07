# Objects

PANTAHUB Objects API for CDN ready storage of blob objects (aka files).

## Start Service

### Development

If you do not set PANTAHUB_S3PATH ennvironment to production we will
use local fake s3. If no path is set it will use the ./local-s3
directory as file storage on local disk

### Production

In order to enable production S3 usage you have to set the PANTAHUB_S3PATH
environment like:

```
export PANTAHUB_S3PATH=production
```

Before starting, set your AWS credentials in your environment:

```
AWS_ACCESS_KEY_ID=XXXX
AWS_SECRET_ACCESS_KEY=YYYYYYYY

export AWS_SECRET_ACCESS_KEY AWS_ACCESS_KEY_ID
```

Now start your server:
```
./pantahub-base
```

## Login

```
TOKEN=`http localhost:12365/auth/login username=user1 password=user1 | json token`
```

... will store access token in TOKEN for requests below

## Upload File

### Register Object

```
# adjust the below to be correct:
upload_file=myfile.jpg
upload_size=12365
upload_shasum256=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

http POST localhost:12365/objects/  Authorization:"Bearer $TOKEN" \
	objectname=$upload_file \
	size=$upload_size \
	sha256sum=$upload_shasum256
	
HTTP/1.1 200 OK
Content-Length: 152
Content-Type: application/json; charset=utf-8
Date: Fri, 19 Aug 2016 12:24:58 GMT
X-Powered-By: go-json-rest

{
    "id": "57b6fa9ac094f67942000002", 
    "mime-type": "", 
    "objectname": "myfile.jpg", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "sha256sum": "xxxxxxxxxxx", 
    "size": "12356"
}

```

### Get Put Url

Use the id from above to get info about where to upload:

```
http GET localhost:12365/objects/57b6fa9ac094f67942000002  Authorization:"Bearer $TOKEN"

HTTP/1.1 200 OK
Content-Length: 941
Content-Type: application/json; charset=utf-8
Date: Fri, 19 Aug 2016 12:26:41 GMT
X-Powered-By: go-json-rest

{
    "expire-time": "900", 
    "id": "57b6fa9ac094f67942000002", 
    "mime-type": "", 
    "now": "1471609601", 
    "objectname": "myfile.jpg", 
    "owner": "prn:pantahub.com:auth:/user1", 
    "sha256sum": "xxxxxxxxxxx", 
    "signed-geturl": "https://systemcloud-001.s3.amazonaws.com/57b6fa9ac094f67942000002?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAJCANUJOIDFTXDLJA%2F20160819%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20160819T122641Z&X-Amz-Expires=900&X-Amz-SignedHeaders=host&X-Amz-Signature=19801ba6781f9b10d7d108cc429e55942497b9bc5b46aafba7325709a82c0029", 
    "signed-puturl": "https://systemcloud-001.s3.amazonaws.com/57b6fa9ac094f67942000002?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAJCANUJOIDFTXDLJA%2F20160819%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20160819T122641Z&X-Amz-Expires=900&X-Amz-SignedHeaders=host&X-Amz-Signature=64ade69ed129f2aedcee9b84cd2e318b4863e2b6518301fbae9e53703c794e73", 
    "size": "12356"
}

SIGNED_GETURL="https://systemcloud-001.s3.amazonaws.com/57b6fa9ac094f67942000002?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAJCANUJOIDFTXDLJA%2F20160819%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20160819T122641Z&X-Amz-Expires=900&X-Amz-SignedHeaders=host&X-Amz-Signature=19801ba6781f9b10d7d108cc429e55942497b9bc5b46aafba7325709a82c0029"
SIGNED_PUTURL="https://systemcloud-001.s3.amazonaws.com/57b6fa9ac094f67942000002?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAJCANUJOIDFTXDLJA%2F20160819%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20160819T122641Z&X-Amz-Expires=900&X-Amz-SignedHeaders=host&X-Amz-Signature=64ade69ed129f2aedcee9b84cd2e318b4863e2b6518301fbae9e53703c794e73"


```

### Put File to S3

```
cat $upload_file | http PUT $SIGNED_PUTURL
```

### Get File from S3

```
http GET $SIGNED_GETURL
```

