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

package utils

import "testing"

func TestParseSuccess(t *testing.T) {

	tests := map[string]interface{}{
		"prn::service:object": &PrnInfo{
			Domain:   "api.pantahub.com",
			Service:  "service",
			Resource: "object",
		},
		"prn:api2.pantahub.com:service:object": &PrnInfo{
			Domain:   "api2.pantahub.com",
			Service:  "service",
			Resource: "object",
		},
		"prn:api2.pantahub.com:service:/object": &PrnInfo{
			Domain:   "api2.pantahub.com",
			Service:  "service",
			Resource: "/object",
		},
		"prn::service:object:subobject": &PrnInfo{
			Domain:   "api.pantahub.com",
			Service:  "service",
			Resource: "object:subobject",
		},
	}

	for k, v := range tests {
		test := Prn(k)
		testInfo := v.(*PrnInfo)

		info, err := test.GetInfo()
		if err != nil {
			t.Errorf("GetInfo Failed - %s: %s", test, err)
			t.Fail()
			return
		}
		if !info.Equals(testInfo) {
			t.Errorf("GetInfo Failed - %s", test)
			t.Fail()
			return
		}
	}
}

func TestParseErrors(t *testing.T) {

	badCases := []string{
		"",
		"something",
		":prn::service:/something",
		"prn:",
		"prn::",
		"prn:::",
		"prn::::",
		"prn:::resource",
		"prn::service:",
	}

	for _, v := range badCases {

		test := Prn(v)
		_, err := test.GetInfo()
		if err == nil {
			t.Errorf("PRN must fail %s: %s", test, err.Error())
			t.Fail()
			return
		}
		if err.Error() == "" {
			t.Error("Error not set.")
			t.Fail()
			return
		}
	}
}
