# Objects

PANTAHUB Objects API for CDN ready storage of blob objects (aka files).

## Start Service

### Development

If you do not set PANTAHUB_STORAGE_DRIVER and PANTAHUB_STORAGE_BASE_PATH ennvironment to production we will
use local fake S3 storage. If no path is set it will use the ./local-s3
directory as file storage on local disk

### Production

In order to enable production S3 usage you have to set the PANTAHUB_STORAGE_DRIVER and PANTAHUB_STORAGE_BASE_PATH
environment like:

```
export PANTAHUB_STORAGE_DRIVER=default
export PANTAHUB_STORAGE_PATH=/my-storage
```

Before starting, set your credentials, bucket and region in your environment:

```
export S3_ACCESS_KEY_ID=...
export S3_SECRET_ACCESS_KEY=...
export S3_REGION=us-east-1
export S3_BUCKET=my-bucket
```

Now start your server:
```
./pantahub-base
```

## Login

Request:

```bash
curl -X POST -H "Content-Type: application/json" \
    --data '{"username":"user1","password":"user1"}' \
    http://localhost:12365/auth/login
```

Response:

```json
{
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE1NDgyODM4MDUsImlkIjoidXNlcjEiLCJuaWNrIjoidXNlcjEiLCJvcmlnX2lhdCI6MTU0ODI4MDIwNSwicHJuIjoicHJuOnBhbnRhaHViLmNvbTphdXRoOi91c2VyMSIsInJvbGVzIjoiYWRtaW4iLCJ0eXBlIjoiVVNFUiJ9.12uPvgekKNC6RPny7_A7eJnBTqfheNep-MSbEQPl0nI"
}
```

... will store access token in TOKEN for requests below

## Upload File

### Register Object

Request:

```bash
curl -X POST -H "Content-Type: application/json" \
    -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE1NDgyODM4MDUsImlkIjoidXNlcjEiLCJuaWNrIjoidXNlcjEiLCJvcmlnX2lhdCI6MTU0ODI4MDIwNSwicHJuIjoicHJuOnBhbnRhaHViLmNvbTphdXRoOi91c2VyMSIsInJvbGVzIjoiYWRtaW4iLCJ0eXBlIjoiVVNFUiJ9.12uPvgekKNC6RPny7_A7eJnBTqfheNep-MSbEQPl0nI" \
    --data '{
        "objectname": "file.txt",
        "size": "133156491", // or "sizeint": 133156491
        "sha256sum": "9a6df421c1bffc2c0178404f3fe1052b034f73a3c5a5bcfde48c5a31267eaeba"
    }' \
    http://localhost:12365/objects/
```

*TIP: You can use `sha256sum` utility to generate the sha256 of a file*

Response:

```json
{
    "id": "9a6df421c1bffc2c0178404f3fe1052b034f73a3c5a5bcfde48c5a31267eaeba",
    "storage-id": "0a2f8cf92e7d9328b965fd7568cda5889f1dcc269883396f80e30deb8f07e797",
    "owner": "prn:pantahub.com:auth:/user1",
    "objectname": "file.txt",
    "sha256sum": "9a6df421c1bffc2c0178404f3fe1052b034f73a3c5a5bcfde48c5a31267eaeba",
    "size": "0",
    "sizeint": 0,
    "mime-type": "",
    "signed-puturl": "http://localhost:12365/local-s3/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIwYTJmOGNmOTJlN2Q5MzI4Yjk2NWZkNzU2OGNkYTU4ODlmMWRjYzI2OTg4MzM5NmY4MGUzMGRlYjhmMDdlNzk3IiwiZXhwIjoxNTQ4MzcxODAzLCJpYXQiOjE1NDgyODU0MDMsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNjUvb2JqZWN0cyIsInN1YiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjEiLCJEaXNwb3NpdGlvbk5hbWUiOiJ2aWRlby5tcDQiLCJTaXplIjowLCJNZXRob2QiOiJQVVQiLCJTaGEiOiI5YTZkZjQyMWMxYmZmYzJjMDE3ODQwNGYzZmUxMDUyYjAzNGY3M2EzYzVhNWJjZmRlNDhjNWEzMTI2N2VhZWJhIn0.IWSGVyWA8lqQb_KYSIVw3uPOfBnX70q6qPW-X1GTekw",
    "signed-geturl": "http://localhost:12365/local-s3/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIwYTJmOGNmOTJlN2Q5MzI4Yjk2NWZkNzU2OGNkYTU4ODlmMWRjYzI2OTg4MzM5NmY4MGUzMGRlYjhmMDdlNzk3IiwiZXhwIjoxNTQ4MzcxODAzLCJpYXQiOjE1NDgyODU0MDMsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNjUvb2JqZWN0cyIsInN1YiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjEiLCJEaXNwb3NpdGlvbk5hbWUiOiJ2aWRlby5tcDQiLCJTaXplIjowLCJNZXRob2QiOiJHRVQiLCJTaGEiOiI5YTZkZjQyMWMxYmZmYzJjMDE3ODQwNGYzZmUxMDUyYjAzNGY3M2EzYzVhNWJjZmRlNDhjNWEzMTI2N2VhZWJhIn0.qQEPPL0mCg_3TCIuhGK6PXZNQYEMFESZmgjv9FujhnI",
    "now": "1548285403",
    "expire-time": "15"
}
```

### Get PUT URL

Use the id from above to get info about where to upload:

Request:

```bash
curl -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE1NDgyODM4MDUsImlkIjoidXNlcjEiLCJuaWNrIjoidXNlcjEiLCJvcmlnX2lhdCI6MTU0ODI4MDIwNSwicHJuIjoicHJuOnBhbnRhaHViLmNvbTphdXRoOi91c2VyMSIsInJvbGVzIjoiYWRtaW4iLCJ0eXBlIjoiVVNFUiJ9.12uPvgekKNC6RPny7_A7eJnBTqfheNep-MSbEQPl0nI" \
     http://localhost:12365/objects/9a6df421c1bffc2c0178404f3fe1052b034f73a3c5a5bcfde48c5a31267eaeba
```

Response:

```json
{
    "id": "9a6df421c1bffc2c0178404f3fe1052b034f73a3c5a5bcfde48c5a31267eaeba",
    "storage-id": "0a2f8cf92e7d9328b965fd7568cda5889f1dcc269883396f80e30deb8f07e797",
    "owner": "prn:pantahub.com:auth:/user1",
    "objectname": "file.txt",
    "sha256sum": "9a6df421c1bffc2c0178404f3fe1052b034f73a3c5a5bcfde48c5a31267eaeba",
    "size": "0",
    "sizeint": 0,
    "mime-type": "",
    "signed-puturl": "http://localhost:12365/local-s3/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIwYTJmOGNmOTJlN2Q5MzI4Yjk2NWZkNzU2OGNkYTU4ODlmMWRjYzI2OTg4MzM5NmY4MGUzMGRlYjhmMDdlNzk3IiwiZXhwIjoxNTQ4MzcyMTc4LCJpYXQiOjE1NDgyODU3NzgsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNjUvb2JqZWN0cyIsInN1YiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjEiLCJEaXNwb3NpdGlvbk5hbWUiOiJ2aWRlby5tcDQiLCJTaXplIjowLCJNZXRob2QiOiJQVVQiLCJTaGEiOiI5YTZkZjQyMWMxYmZmYzJjMDE3ODQwNGYzZmUxMDUyYjAzNGY3M2EzYzVhNWJjZmRlNDhjNWEzMTI2N2VhZWJhIn0.WkMhS87NlTc2emmhvJvLYMVsfHkhndxwMb4JdfD8bHI",
    "signed-geturl": "http://localhost:12365/local-s3/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIwYTJmOGNmOTJlN2Q5MzI4Yjk2NWZkNzU2OGNkYTU4ODlmMWRjYzI2OTg4MzM5NmY4MGUzMGRlYjhmMDdlNzk3IiwiZXhwIjoxNTQ4MzcyMTc4LCJpYXQiOjE1NDgyODU3NzgsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNjUvb2JqZWN0cyIsInN1YiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjEiLCJEaXNwb3NpdGlvbk5hbWUiOiJ2aWRlby5tcDQiLCJTaXplIjowLCJNZXRob2QiOiJHRVQiLCJTaGEiOiI5YTZkZjQyMWMxYmZmYzJjMDE3ODQwNGYzZmUxMDUyYjAzNGY3M2EzYzVhNWJjZmRlNDhjNWEzMTI2N2VhZWJhIn0.mCC6eZHyG0wuTjWh-DFBR2pLxGpTYb3_vkRpAQulzCU",
    "now": "1548285778",
    "expire-time": "15"
}
```

### PUT File to Storage

Request:

```bash
curl -X PUT -T file.txt "http://localhost:12365/local-s3/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiIwYTJmOGNmOTJlN2Q5MzI4Yjk2NWZkNzU2OGNkYTU4ODlmMWRjYzI2OTg4MzM5NmY4MGUzMGRlYjhmMDdlNzk3IiwiZXhwIjoxNTQ4MzcyMTc4LCJpYXQiOjE1NDgyODU3NzgsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNjUvb2JqZWN0cyIsInN1YiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjEiLCJEaXNwb3NpdGlvbk5hbWUiOiJ2aWRlby5tcDQiLCJTaXplIjowLCJNZXRob2QiOiJQVVQiLCJTaGEiOiI5YTZkZjQyMWMxYmZmYzJjMDE3ODQwNGYzZmUxMDUyYjAzNGY3M2EzYzVhNWJjZmRlNDhjNWEzMTI2N2VhZWJhIn0.WkMhS87NlTc2emmhvJvLYMVsfHkhndxwMb4JdfD8bHI"
```

Response:

Empty response

### Get File from S3

Request:

```bash
curl "http://localhost:12365/local-s3/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJlMDQ0OWZjODQ1MWI0YmQ2N2IyYWE3YmFiZDQ3YzM0YzNjMGUzYmI3ODkwYzBjOTYwODVlZmM5MWQwZGFjNjUxIiwiZXhwIjoxNTQ4Mzc4MjM0LCJpYXQiOjE1NDgyOTE4MzQsImlzcyI6Imh0dHA6Ly9sb2NhbGhvc3Q6MTIzNjUvb2JqZWN0cyIsInN1YiI6InBybjpwYW50YWh1Yi5jb206YXV0aDovdXNlcjEiLCJEaXNwb3NpdGlvbk5hbWUiOiJtYWluLmdvIiwiU2l6ZSI6MTQ1MiwiTWV0aG9kIjoiR0VUIiwiU2hhIjoiMGJlYTVjZTkzYmViMTczMzk2NjhmNjI0YTFkZjk0MDFkYmMzMTliNzlmZDkyZTlhYjZlMjViMmMwMDAyMDY0NyJ9.IG4blrxGEqdAeeU8ZU8nBwXnKsZ_EyVqaYqrDSoGAbM"
```

Response:

File previously uploaded
