// Package utils is licensed as follows:
//
// Copyright 2018  Pantacor Ltd.
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

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Prn string

type PrnParseError string

func (s PrnParseError) Error() string {
	return string(s)
}

type PrnInfo struct {
	Domain   string
	Service  string
	Resource string
}

// XXX: make this a nice prn helper tool
func PrnGetId(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1:]
}

func IdGetPrn(id primitive.ObjectID, serviceName string) string {
	return "prn:::" + serviceName + ":/" + id.Hex()
}

func (p *Prn) GetInfo() (*PrnInfo, error) {
	if !strings.HasPrefix(string(*p), "prn:") {
		errstr := fmt.Sprintf("ERROR: prn parse prn: prefix missing - %s", *p)
		return nil, PrnParseError(errstr)
	}

	rs := PrnInfo{}

	i := strings.Index(string(*p)[4:], ":")
	if i == 0 {
		rs.Domain = "api.pantahub.com"
	} else if i > 0 {
		rs.Domain = string(*p)[4 : 4+i]
	} else {
		errstr := fmt.Sprintf("ERROR: prn parse: domain missing - %s", *p)
		return nil, PrnParseError(errstr)
	}

	if len(string(*p)) <= 4+i+1 {
		errstr := fmt.Sprintf("ERROR: prn parse: service start missing - %s", *p)
		return nil, PrnParseError(errstr)
	}

	j := strings.Index(string(*p)[4+i+1:], ":")

	if j > 0 {
		rs.Service = string(*p)[4+i+1 : 4+i+1+j]
	} else {
		errstr := fmt.Sprintf("ERROR: prn parse: service end missing - %s", *p)
		return nil, PrnParseError(errstr)
	}

	if len(string(*p)) <= 4+i+1+j+1 {
		errstr := fmt.Sprintf("ERROR: prn parse: resource start missing - %s", *p)
		return nil, PrnParseError(errstr)
	}

	rs.Resource = string(*p)[4+i+1+j+1:]

	return &rs, nil
}

func (p *PrnInfo) Equals(c *PrnInfo) bool {
	return p.Domain == c.Domain &&
		p.Service == c.Service &&
		p.Resource == c.Resource
}
