//
// Copyright 2019 Pantacor Ltd.
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
package s3

import "log"

type S3ConnectionParameters struct {
	AccessKey string
	SecretKey string
	Region    string
	Bucket    string
	Endpoint  string
}

func (s S3ConnectionParameters) IsValid() bool {
	if s.AccessKey == "" {
		log.Println("Empty S3 AccessKey")
		return false
	}

	if s.SecretKey == "" {
		log.Println("Empty S3 SecretKey")
		return false
	}

	if s.Region == "" {
		log.Println("Empty S3 region")
		return false
	}

	if s.Bucket == "" {
		log.Println("Empty S3 bucket")
		return false
	}

	return true
}
