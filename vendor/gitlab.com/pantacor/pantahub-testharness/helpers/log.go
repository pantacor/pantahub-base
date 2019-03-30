//
// Copyright 2019  Pantacor Ltd.
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
package helpers

import (
	"encoding/json"
	"testing"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-gc/models"
	"gopkg.in/mgo.v2/bson"
)

// CreateLog : Create A Log
func CreateLog(
	t *testing.T,
	dToken string,
	logData map[string]interface{},
) (
	[]interface{},
	*resty.Response,
) {
	response := []interface{}{}
	APIEndPoint := BaseAPIUrl + "/logs/"
	res, err := resty.R().
		SetBody(logData).
		SetAuthToken(dToken).
		Post(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// ListLogs : List logs
func ListLogs(t *testing.T) (
	map[string]interface{},
	*resty.Response,
) {
	response := map[string]interface{}{}
	APIEndPoint := BaseAPIUrl + "/logs/"
	res, err := resty.R().
		SetAuthToken(UTOKEN).
		Get(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	err = json.Unmarshal(res.Body(), &response)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}
	return response, res
}

// DeleteAllPersonalLogs : Delete All Personal Logs
func DeleteAllPersonalLogs(t *testing.T) bool {
	db := db.Session
	c := db.C("pantahub_objects")
	_, err := c.RemoveAll(bson.M{})
	if err != nil {
		t.Errorf("Error on Removing: " + err.Error())
		t.Fail()
		return false
	}
	Objects = []models.Object{}
	return true
}
