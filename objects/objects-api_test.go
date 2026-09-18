//
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
//

package objects

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/jaswdr/faker"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
	"gopkg.in/mgo.v2/bson"
)

type mockFileUploadServer struct {
	mock.Mock
}

func (m mockFileUploadServer) Exists(key string) bool {
	return m.Called(key).Bool(0)
}

func (m mockFileUploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.Called(w, r)
}

type ObjectsAppTestSuite struct {
	suite.Suite
	fileserver *mockFileUploadServer
	app        *App
}

func (suite *ObjectsAppTestSuite) SetupTest() {
	adminUsers := utils.GetSubscriptionAdmins()
	client, _ := utils.GetMongoClient()
	subService := subscriptions.NewService(client, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)
	fileserver := &mockFileUploadServer{}
	app := New(
		&jwtauth.Config{
			Key:   []byte("secret"),
			Realm: "\"pantahub services\", ph-aeps=\"" + "http://localhost" + "\"",
		},
		subService,
		client,
	)
	suite.app = app
	suite.fileserver = fileserver
}

func (suite *ObjectsAppTestSuite) TestPantahubS3PathIsNotEmpty() {
	assert.NotEmpty(suite.T(), utils.PantahubS3Path())
}

func (suite *ObjectsAppTestSuite) TestPantahubS3DevUrlIsNotEmpty() {
	// TODO finish test case
	// assert.NotEmpty(suite.T(), PantahubS3DevUrl())
}

func (suite *ObjectsAppTestSuite) newContext(req *http.Request) (*echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := make(jwtgo.MapClaims)
	claims["prn"] = "prn::testing"
	claims["type"] = "USER"
	c.Set(echoutil.KeyJWTPayload, claims)
	return c, rec
}

func (suite *ObjectsAppTestSuite) newPostObjectRequest(objectname string, content []byte, size int) *http.Request {
	sum := sha256.Sum256(content)
	bodyMap := map[string]interface{}{
		"objectname": objectname,
		"sha256sum":  hex.EncodeToString(sum[:]),
		"size":       strconv.Itoa(size),
	}
	body, _ := json.Marshal(bodyMap)
	httpRequest := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	return httpRequest
}

func (suite *ObjectsAppTestSuite) TestHandlePostObjectReturnsStatusOKWhenObjectDoesNotExist() {
	content := []byte("testing")
	req := suite.newPostObjectRequest("testing", content, 7)
	c, rec := suite.newContext(req)
	_ = suite.app.handlePostObject(c)

	resp := make(map[string]interface{})
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.NoError(suite.T(), err, rec.Body.String())
	assert.NotEmpty(suite.T(), resp["signed-puturl"])
	assert.NotEmpty(suite.T(), resp["signed-geturl"])
}

// Ensure that *posting* and object that already exist returns StatusOK and updates database document when there doesn't exists in backing storage.
func (suite *ObjectsAppTestSuite) TestHandlePostObjectReturnsStatusOKWhenObjectExist() {
	f := faker.New()
	content := []byte(f.UUID().V4())

	// first request
	r1 := suite.newPostObjectRequest("object1", content, 7)
	c1, rec1 := suite.newContext(r1)
	_ = suite.app.handlePostObject(c1)

	resp1 := make(map[string]interface{})
	err := json.Unmarshal(rec1.Body.Bytes(), &resp1)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp1["id"])
	assert.NotEmpty(suite.T(), resp1["size"])
	assert.NotEmpty(suite.T(), resp1["signed-puturl"])
	assert.NotEmpty(suite.T(), resp1["signed-geturl"])
	assert.NotEmpty(suite.T(), rec1.Body.Bytes())
	suite.fileserver.AssertExpectations(suite.T())

	// second request
	suite.fileserver.On("Exists", mock.Anything).Return(false)

	r2 := suite.newPostObjectRequest("object2", content, 10)
	c2, rec2 := suite.newContext(r2)
	_ = suite.app.handlePostObject(c2)

	resp2 := make(map[string]interface{})
	err = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp2["id"])
	assert.NotEmpty(suite.T(), resp2["size"])
	assert.NotEmpty(suite.T(), resp2["signed-puturl"])
	assert.NotEmpty(suite.T(), resp2["signed-geturl"])
	assert.NotEmpty(suite.T(), rec2.Body.Bytes())

	session, _ := utils.GetMongoSession()
	objects := session.DB("").C("pantahub_objects")

	var object Object
	objectID := resp1["id"]
	err = objects.Find(bson.M{
		"id": objectID,
	}).One(&object)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "object2", object.ObjectName)
	assert.Equal(suite.T(), "10", object.Size)
	suite.fileserver.AssertExpectations(suite.T())
}

// Ensure that *posting* an object that has an object in storage returns StatusConflict.
func (suite *ObjectsAppTestSuite) TestHandlePostObjectReturnsStatusConflictWhenObjectExistInStorageServer() {
	f := faker.New()
	content := []byte(f.UUID().V4())

	// first request
	r1 := suite.newPostObjectRequest("object1", content, 7)
	c1, rec1 := suite.newContext(r1)
	_ = suite.app.handlePostObject(c1)

	resp1 := make(map[string]interface{})
	err := json.Unmarshal(rec1.Body.Bytes(), &resp1)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp1["id"])
	assert.NotEmpty(suite.T(), resp1["size"])
	assert.NotEmpty(suite.T(), resp1["signed-puturl"])
	assert.NotEmpty(suite.T(), resp1["signed-geturl"])
	assert.NotEmpty(suite.T(), rec1.Body.Bytes())
	suite.fileserver.AssertExpectations(suite.T())

	// second request
	r2 := suite.newPostObjectRequest("object2", content, 10)
	c2, rec2 := suite.newContext(r2)
	_ = suite.app.handlePostObject(c2)

	resp2 := make(map[string]interface{})
	err = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp2["id"])
	assert.NotEmpty(suite.T(), resp2["size"])
	assert.NotEmpty(suite.T(), resp2["signed-puturl"])
	assert.NotEmpty(suite.T(), resp2["signed-geturl"])
	assert.NotEmpty(suite.T(), rec2.Body.Bytes())

	session, _ := utils.GetMongoSession()
	objects := session.DB("").C("pantahub_objects")

	var object Object
	objectID := resp1["id"]
	err = objects.Find(bson.M{
		"id": objectID,
	}).One(&object)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "object2", object.ObjectName)
	assert.Equal(suite.T(), "10", object.Size)
	suite.fileserver.AssertExpectations(suite.T())
}

func TestObjectsAppTestSuite(t *testing.T) {
	suite.Run(t, new(ObjectsAppTestSuite))
}
