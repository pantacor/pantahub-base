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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type LocalFileUploadServerTestSuite struct {
	suite.Suite
	server *LocalFileUploadServer
}

func (suite *LocalFileUploadServerTestSuite) SetupTest() {
	basePath := utils.PantahubS3Path()
	fileServer := http.FileServer(http.Dir(basePath))
	suite.server = &LocalFileUploadServer{fileServer: fileServer}
}

func (suite *LocalFileUploadServerTestSuite) TestOpenForWrite() {
	file, err := suite.server.openForWrite("testing")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), file)
}

func (suite *LocalFileUploadServerTestSuite) generateSignedToken(method string, content []byte) string {
	sha := fmt.Sprintf("%x", sha256.Sum256(content))
	o := NewObjectAccessForSec(
		"testing",
		method,
		int64(len(content)),
		sha,
		"testing",
		"testing",
		"testing",
		1,
	)

	token, err := o.Sign()
	if err != nil {
		panic(err)
	}

	return token

}

func (suite *LocalFileUploadServerTestSuite) TestPushObjectMatchingSHA() {
	content := []byte("testing")
	token := suite.generateSignedToken(http.MethodPut, content)
	req, err := http.NewRequest(http.MethodPut, "/local-s3/"+token, bytes.NewReader(content))
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusOK, rr.Code)
}

func (suite *LocalFileUploadServerTestSuite) TestPushObjectNotMatchingSHA() {
	correct := []byte("content")
	invalid := []byte("invalid-content")
	token := suite.generateSignedToken(http.MethodPut, correct)
	req, err := http.NewRequest(http.MethodPut, "/local-s3/"+token, bytes.NewReader(invalid))
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusBadRequest, rr.Code)
}

func (suite *LocalFileUploadServerTestSuite) TestGetObjectNotUploaded() {
	token := suite.generateSignedToken(http.MethodPut, nil)
	req, err := http.NewRequest(http.MethodGet, "/local-s3/"+token, nil)
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusForbidden, rr.Code)
}

func (suite *LocalFileUploadServerTestSuite) TestGetObjectWithInvalidToken() {
	req, err := http.NewRequest(http.MethodGet, "/local-s3/invalid-token", nil)
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusForbidden, rr.Code)
}

func TestLocalFileUploadServerTestSuite(t *testing.T) {
	suite.Run(t, new(LocalFileUploadServerTestSuite))
}