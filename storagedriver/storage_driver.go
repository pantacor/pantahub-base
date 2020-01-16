//
// Copyright 2020  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package storagedriver

import (
	"gitlab.com/pantacor/pantahub-base/s3"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// StorageDriver storage drive interface
type StorageDriver interface {
	Exists(key string) bool
}

// FromEnv get storage driver from environment
func FromEnv() StorageDriver {
	switch utils.GetEnv(utils.EnvPantahubStorageDriver) {
	case "s3":
		connParams := s3.ConnectionParameters{
			AccessKey: utils.GetEnv(utils.EnvPantahubS3AccessKeyID),
			SecretKey: utils.GetEnv(utils.EnvPantahubS3SecretAccessKeyID),
			Region:    utils.GetEnv(utils.EnvPantahubS3Region),
			Bucket:    utils.GetEnv(utils.EnvPantahubS3Bucket),
			Endpoint:  utils.GetEnv(utils.EnvPantahubS3Endpoint),
		}

		return s3.New(connParams)
	default:
		return NewLocalStorageDriver()
	}
}
