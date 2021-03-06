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

// BsonQuoteMap create a new map of quotes with escaped indexes
func BsonQuoteMap(m *map[string]interface{}) map[string]interface{} {
	escapedMap := map[string]interface{}{}
	for k, v := range *m {
		nk := strings.Replace(k, ".", "\uFF2E", -1)
		escapedMap[nk] = v
	}
	return escapedMap
}

// BsonUnquoteMap create a new map of quotes with unescaped indexes
func BsonUnquoteMap(m *map[string]interface{}) map[string]interface{} {
	escapedMap := map[string]interface{}{}
	for k, v := range *m {
		nk := strings.Replace(k, "\uFF2E", ".", -1)
		escapedMap[nk] = v
	}
	return escapedMap
}
