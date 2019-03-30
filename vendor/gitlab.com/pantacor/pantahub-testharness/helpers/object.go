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
package helpers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/go-resty/resty"
	"gitlab.com/pantacor/pantahub-gc/db"
	"gitlab.com/pantacor/pantahub-gc/models"
	"gopkg.in/mgo.v2/bson"
)

// GenerateObjectSha : Generate ObjectSha string
func GenerateObjectSha() string {
	randomString := RandStringRunes(10)
	arr := sha256.Sum256([]byte(randomString))
	sha := hex.EncodeToString(arr[:])
	return sha
}

// CreateObject : Create new Object
func CreateObject(
	t *testing.T,
	sha string,
) (
	objectSha string,
	object models.Object,
	res *resty.Response,
) {
	APIEndPoint := BaseAPIUrl + "/objects/"
	res, err := resty.R().SetAuthToken(UTOKEN).SetBody(map[string]string{
		"sha256sum": sha,
	}).Post(APIEndPoint)
	if err != nil {
		t.Errorf("internal error calling test server " + err.Error())
		t.Fail()
	}
	object = models.Object{}
	err = json.Unmarshal(res.Body(), &object)
	if err != nil {
		t.Errorf(err.Error())
		t.Fail()
	}

	Objects = append(Objects, object)
	ObjectsCount++
	return object.Sha, object, res
}

// DeleteAllObjects : Delete All Objects
func DeleteAllObjects(t *testing.T) bool {
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

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// RandStringRunes : Generate Random string
func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

// UpdateObjectGarbageRemovalDate : Update Object Garbage Removal Date
func UpdateObjectGarbageRemovalDate(t *testing.T, object models.Object) bool {
	GarbageRemovalAt := time.Now().Local().Add(-time.Minute * time.Duration(1)) //decrease 1 min
	db := db.Session
	c := db.C("pantahub_objects")
	err := c.Update(
		bson.M{"id": object.ID},
		bson.M{"$set": bson.M{
			"garbage_removal_at": GarbageRemovalAt,
		}})
	if err != nil {
		t.Errorf("internal error calling test server: " + err.Error())
		t.Fail()
		return false
	}
	return true
}

// ListObjects : List Objects
func ListObjects(t *testing.T) (
	response []interface{},
	res *resty.Response,
) {
	response = []interface{}{}
	APIEndPoint := BaseAPIUrl + "/objects/"
	res, err := resty.R().SetAuthToken(UTOKEN).Get(APIEndPoint)
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
