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

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/fundapps/go-json-rest-middleware-jwt"
	"github.com/jaswdr/faker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/utils"
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
	app        *ObjectsApp
}

func (suite *ObjectsAppTestSuite) SetupTest() {
	adminUsers := utils.GetSubscriptionAdmins()
	session, _ := utils.GetMongoSession()
	subService := subscriptions.NewService(session, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)
	fileserver := &mockFileUploadServer{}
	app := New(&jwt.JWTMiddleware{
		Key:   []byte("secret"),
		Realm: "\"pantahub services\", ph-aeps=\"" + "http://localhost" + "\"",
		Authenticator: func(userID, password string) bool {
			return false
		}, subService, session, fileserver})
	http.Handle("/objects/", http.StripPrefix("/objects", app.Api.MakeHandler()))
	suite.app = app
	suite.fileserver = fileserver
}

func (suite *ObjectsAppTestSuite) TestPantahubS3PathIsNotEmpty() {
	assert.NotEmpty(suite.T(), utils.PantahubS3Path())
}

func (suite *ObjectsAppTestSuite) TestPantahubS3DevUrlIsNotEmpty() {
	assert.NotEmpty(suite.T(), PantahubS3DevUrl())
}

type mockResponseWriter struct {
	mock.Mock
	Recorder *httptest.ResponseRecorder
}

func (m mockResponseWriter) Header() http.Header {
	return m.Called().Get(0).(http.Header)
}

func (m mockResponseWriter) WriteJson(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	m.Recorder.Write(b)

	return m.Called(v).Error(0)
}

func (m mockResponseWriter) WriteHeader(code int) {
	m.Called(code)
}

func (m mockResponseWriter) Count() uint64 {
	return m.Called().Get(0).(uint64)
}

func (m mockResponseWriter) EncodeJson(v interface{}) ([]byte, error) {
	args := m.Called(v)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]byte), args.Error(1)
}

func (suite *ObjectsAppTestSuite) newPostObjectRequest(objectname string, content []byte, size int) rest.Request {
	sum := sha256.Sum256(content)
	bodyMap := map[string]interface{}{
		"objectname": objectname,
		"sha256sum":  hex.EncodeToString(sum[:]),
		"size":       strconv.Itoa(size),
	}
	body, _ := json.Marshal(bodyMap)
	httpRequest := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	r := rest.Request{
		httpRequest,
		nil,
		map[string]interface{}{},
	}

	claims := make(jwtgo.MapClaims)
	claims["prn"] = "prn::testing"
	claims["type"] = "USER"

	r.Env = make(map[string]interface{})
	r.Env["JWT_PAYLOAD"] = claims
	return r
}

func (suite *ObjectsAppTestSuite) TestHandlePostObjectReturnsStatusOKWhenObjectDoesNotExist() {
	content := []byte("testing")
	r := suite.newPostObjectRequest("testing", content, 7)
	w := mockResponseWriter{Recorder: httptest.NewRecorder()}
	w.On("WriteHeader", 200).Return(nil)
	w.On("WriteJson", mock.Anything).Return(nil)
	suite.app.handle_postobject(w, &r)

	resp := make(map[string]interface{})
	err := json.Unmarshal(w.Recorder.Body.Bytes(), &resp)
	assert.NoError(suite.T(), err, w.Recorder.Body.String())
	assert.NotEmpty(suite.T(), resp["signed-puturl"])
	assert.NotEmpty(suite.T(), resp["signed-geturl"])
}

// Ensure that *posting* and object that already exist returns StatusOK and updates database document when there doesn't exists in backing storage.
func (suite *ObjectsAppTestSuite) TestHandlePostObjectReturnsStatusOKWhenObjectExist() {
	f := faker.New()
	content := []byte(f.UUID().V4())

	// first request
	w1 := mockResponseWriter{Recorder: httptest.NewRecorder()}
	w1.On("WriteJson", mock.Anything).Return(nil)

	r1 := suite.newPostObjectRequest("object1", content, 7)
	suite.app.handle_postobject(w1, &r1)

	resp1 := make(map[string]interface{})
	err := json.Unmarshal(w1.Recorder.Body.Bytes(), &resp1)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp1["id"])
	assert.NotEmpty(suite.T(), resp1["size"])
	assert.NotEmpty(suite.T(), resp1["signed-puturl"])
	assert.NotEmpty(suite.T(), resp1["signed-geturl"])
	w1.AssertExpectations(suite.T())
	suite.fileserver.AssertExpectations(suite.T())

	// second request
	suite.fileserver.On("Exists", mock.Anything).Return(false)
	w2 := mockResponseWriter{Recorder: httptest.NewRecorder()}
	w2.On("WriteJson", mock.Anything).Return(nil)

	r2 := suite.newPostObjectRequest("object2", content, 10)
	suite.app.handle_postobject(w2, &r2)

	resp2 := make(map[string]interface{})
	err = json.Unmarshal(w2.Recorder.Body.Bytes(), &resp2)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp2["id"])
	assert.NotEmpty(suite.T(), resp2["size"])
	assert.NotEmpty(suite.T(), resp2["signed-puturl"])
	assert.NotEmpty(suite.T(), resp2["signed-geturl"])
	w2.AssertExpectations(suite.T())

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
	w1 := mockResponseWriter{Recorder: httptest.NewRecorder()}
	w1.On("WriteJson", mock.Anything).Return(nil)

	r1 := suite.newPostObjectRequest("object1", content, 7)
	suite.app.handle_postobject(w1, &r1)

	resp1 := make(map[string]interface{})
	err := json.Unmarshal(w1.Recorder.Body.Bytes(), &resp1)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp1["id"])
	assert.NotEmpty(suite.T(), resp1["size"])
	assert.NotEmpty(suite.T(), resp1["signed-puturl"])
	assert.NotEmpty(suite.T(), resp1["signed-geturl"])
	w1.AssertExpectations(suite.T())
	suite.fileserver.AssertExpectations(suite.T())

	// second request
	w2 := mockResponseWriter{Recorder: httptest.NewRecorder()}
	w2.On("WriteJson", mock.Anything).Return(nil)

	r2 := suite.newPostObjectRequest("object2", content, 10)
	suite.app.handle_postobject(w2, &r2)

	resp2 := make(map[string]interface{})
	err = json.Unmarshal(w2.Recorder.Body.Bytes(), &resp2)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), resp2["id"])
	assert.NotEmpty(suite.T(), resp2["size"])
	assert.NotEmpty(suite.T(), resp2["signed-puturl"])
	assert.NotEmpty(suite.T(), resp2["signed-geturl"])
	w2.AssertExpectations(suite.T())

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
