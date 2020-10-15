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

package utils

import (
	"net/http"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/accounts"
)

type UserTypeFilterMiddleware struct {
	filterTypes []accounts.AccountType
}

func (m *UserTypeFilterMiddleware) MiddlewareFunc(handler rest.HandlerFunc) rest.HandlerFunc {
	return UserTypeFilter(m.filterTypes, handler)
}

func InitUserTypeFilterMiddleware(filterTypes []accounts.AccountType) *UserTypeFilterMiddleware {
	return &UserTypeFilterMiddleware{
		filterTypes,
	}
}

// UserTypeFilter filter request by user type
func UserTypeFilter(filterTypes []accounts.AccountType, handler rest.HandlerFunc) rest.HandlerFunc {
	return func(w rest.ResponseWriter, r *rest.Request) {
		authInfo := GetAuthInfo(r)
		if authInfo != nil && len(filterTypes) > 0 {
			if _, found := find(filterTypes, authInfo.CallerType); !found {
				RestErrorWrapper(w, "Type of user can't realize that action", http.StatusForbidden)
				return
			}
		}
		handler(w, r)
	}
}

func find(slice []accounts.AccountType, val string) (int, bool) {
	for i, item := range slice {
		if string(item) == val {
			return i, true
		}
	}
	return -1, false
}
