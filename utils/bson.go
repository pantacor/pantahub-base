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
	"fmt"
	"strings"

	cjson "github.com/gibson042/canonicaljson-go"
	"github.com/microcosm-cc/bluemonday"
)

func bsonQuoteMap(m *map[string]interface{}) map[string]interface{} {
	escapedMap := map[string]interface{}{}
	for k, v := range *m {
		nk := BsonQuoteAndDollar(k)
		if strVal, ok := v.(string); ok {
			escapedMap[nk] = QuoteDollar(strVal)
		} else {
			escapedMap[nk] = v
		}
	}
	return escapedMap
}

// BsonUnquoteMap create a new map of quotes with unescaped indexes
func bsonUnquoteMap(m *map[string]interface{}) map[string]interface{} {
	escapedMap := map[string]interface{}{}
	for k, v := range *m {
		nk := BsonUnquoteAndDollar(k)
		if strVal, ok := v.(string); ok {
			escapedMap[nk] = UnquoteDollar(strVal)
		} else {
			escapedMap[nk] = v
		}
	}
	return escapedMap
}

// BsonQuoteMap create a new map of quotes with escaped indexes
func BsonQuoteMap(m *map[string]interface{}) map[string]interface{} {
	quoted := bsonQuoteMap(m)
	b, err := cjson.Marshal(quoted)
	if err != nil {
		fmt.Println("error marshal on BsonQuoteMap")
		fmt.Println(err.Error())

		return quoted
	}

	escapedMap := map[string]interface{}{}
	err = cjson.Unmarshal([]byte(QuoteDollar(string(b))), &escapedMap)
	if err != nil {
		fmt.Println("error Unmarshal on BsonQuoteMap")
		fmt.Println(err.Error())

		return quoted
	}

	return escapedMap
}

// BsonUnquoteMap create a new map of quotes with unescaped indexes
func BsonUnquoteMap(m *map[string]interface{}) map[string]interface{} {
	unquoted := bsonUnquoteMap(m)
	b, err := cjson.Marshal(unquoted)
	if err != nil {
		fmt.Println("error marshal on BsonUnquoteMap")
		fmt.Println(err.Error())

		return unquoted
	}

	escapedMap := map[string]interface{}{}
	err = cjson.Unmarshal([]byte(UnquoteDollar(string(b))), &escapedMap)
	if err != nil {
		fmt.Println("error Unmarshal on BsonUnquoteMap")
		fmt.Println(err.Error())

		return unquoted
	}

	return escapedMap
}

func BsonUnquoteAndDollar(s string) string {
	return BsonUnquote(UnquoteDollar(s))
}

func BsonQuoteAndDollar(s string) string {
	return BsonQuote(QuoteDollar(s))
}

// BsonUnquote unquote a string
func BsonUnquote(s string) string {
	return strings.Replace(s, "\uFF2E", ".", -1)
}

// BsonQuote quote a string
func BsonQuote(s string) string {
	return strings.Replace(s, ".", "\uFF2E", -1)
}

func UnquoteDollar(s string) string {
	return strings.Replace(s, "\uFFE0", "$", -1)
}

func QuoteDollar(s string) string {
	return strings.Replace(s, "$", "\uFFE0", -1)
}

func SanitizeInput(input string) string {
	p := bluemonday.UGCPolicy() // Allows safe HTML but removes scripts
	return p.Sanitize(input)
}
