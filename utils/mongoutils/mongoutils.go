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
