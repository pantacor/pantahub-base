//
// Copyright 2017  Pantacor Ltd.
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
package utils

import (
	"strings"
)

func GetApiEndpoint(localEndpoint string) string {
	// XXX: this is a hack for nginx proxying apparently not setting right scheme
	urlScheme := GetEnv(ENV_PANTAHUB_SCHEME)
	urlHost := GetEnv(ENV_PANTAHUB_HOST)
	urlPort := GetEnv(ENV_PANTAHUB_PORT)
	urlApiVersion := GetEnv(ENV_PANTAHUB_APIVERSION)

	url := urlScheme + "://" + urlHost
	if urlPort != "" {
		url += ":" + urlPort
	}
	if urlApiVersion != "" {
		url += "/" + urlApiVersion
	}
	if localEndpoint == "" {
		return url
	}
	if !strings.HasPrefix(localEndpoint, "/") {
		url += "/"
	}
	url += localEndpoint
	return url
}
