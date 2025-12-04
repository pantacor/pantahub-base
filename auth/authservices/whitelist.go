// Copyright 2017-2020  Pantacor Ltd.
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

package authservices

import (
	"strings"

	"gitlab.com/pantacor/pantahub-base/utils"
)

// IsEmailDomainAllowed checks if an email's domain is in the allowed list
func IsEmailDomainAllowed(email string) bool {
	allowedDomains := utils.GetEnv(utils.EnvPantahubAuthAllowedDomains)
	if allowedDomains == "" {
		return true
	}

	domains := strings.Split(allowedDomains, ",")
	emailDomain := strings.Split(email, "@")[1]

	for _, domain := range domains {
		if strings.TrimSpace(domain) == emailDomain {
			return true
		}
	}

	return false
}
