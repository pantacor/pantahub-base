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
			t.Errorf("PRN must fail %s: %s (error:%s)", test, err.Error())
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
