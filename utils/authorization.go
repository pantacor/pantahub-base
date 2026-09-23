//
// Copyright (c) 2017-2026 Pantacor Ltd.
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
	"gitlab.com/pantacor/pantahub-base/accounts"
)

// AllowsCallerType reports whether callerType is in filterTypes; shared with utils/echoutil.
func AllowsCallerType(filterTypes []accounts.AccountType, callerType string) bool {
	_, found := find(filterTypes, callerType)
	return found
}

func find(slice []accounts.AccountType, val string) (int, bool) {
	for i, item := range slice {
		if string(item) == val {
			return i, true
		}
	}
	return -1, false
}
