package storagedriver

import (
	"gitlab.com/pantacor/pantahub-base/s3"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type StorageDriver interface {
	Exists(key string) bool
}

func FromEnv() StorageDriver {
	switch utils.GetEnv(utils.ENV_PANTAHUB_STORAGE_DRIVER) {
	case "s3":
		connParams := s3.S3ConnectionParameters{
			AccessKey: utils.GetEnv(utils.ENV_PANTAHUB_S3_ACCESS_KEY_ID),
			SecretKey: utils.GetEnv(utils.ENV_PANTAHUB_S3_SECRET_ACCESS_KEY),
			Region:    utils.GetEnv(utils.ENV_PANTAHUB_S3_REGION),
			Bucket:    utils.GetEnv(utils.ENV_PANTAHUB_S3_BUCKET),
			Endpoint:  utils.GetEnv(utils.ENV_PANTAHUB_S3_ENDPOINT),
		}

		return s3.NewS3(connParams)
	default:
		return NewLocalStorageDriver()
	}
}
