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

import (
	"encoding/json"
	"fmt"
	"log"
)

// ConnectionParameters s3 connection parameters
type ConnectionParameters struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Endpoint  string `json:"endpoint"`
}

// GetCPFromJsonByRegion get from the json configuration string the configuration parameters
func GetCPFromJsonByRegion(src, region string) (*ConnectionParameters, error) {
	connections := map[string]ConnectionParameters{}
	if err := json.Unmarshal([]byte(src), &connections); err != nil {
		return nil, err
	}

	value, ok := connections[region]
	if !ok {
		return nil, fmt.Errorf("configuration not found for region %s", region)
	}

	return &value, nil
}

// IsValid check connection parameters to be valid
func (s ConnectionParameters) IsValid() bool {
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
