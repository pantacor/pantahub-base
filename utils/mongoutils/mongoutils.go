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

package mongoutils

import (
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
)

// IsNotFound resource not found
func IsNotFound(err error) bool {
	return err == mongo.ErrNoDocuments
}

// IsKeyDuplicated test if a key already exist on storage
func IsKeyDuplicated(err error) bool {
	return strings.Contains(err.Error(), "duplicate key error collection")
}

// IsDuplicateKey test if a key already exist on storage
func IsDuplicateKey(key string, err error) bool {
	return strings.Contains(err.Error(), "duplicate key error collection") &&
		strings.Contains(err.Error(), "index: "+key)

}

// query operators tolerated in client-supplied filters: comparisons and
// boolean composition only. Notably absent: $where, $function, $accumulator,
// $expr — those execute server-side and turn a filter into code execution.
var allowedFilterOperators = map[string]bool{
	"$eq": true, "$ne": true, "$gt": true, "$gte": true, "$lt": true,
	"$lte": true, "$in": true, "$nin": true, "$exists": true, "$type": true,
	"$and": true, "$or": true, "$nor": true, "$not": true,
	"$elemMatch": true, "$size": true, "$all": true,
	"$regex": true, "$options": true,
}

// ValidateClientFilter walks a filter decoded from client JSON and rejects
// any `$`-prefixed key that is not a plain comparison/logic operator, at any
// depth. Use it on every filter that is unmarshalled straight into a Find.
func ValidateClientFilter(v interface{}) error {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if strings.HasPrefix(k, "$") && !allowedFilterOperators[k] {
				return errors.New("filter operator not allowed: " + k)
			}
			if err := ValidateClientFilter(val); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, val := range t {
			if err := ValidateClientFilter(val); err != nil {
				return err
			}
		}
	}
	return nil
}
