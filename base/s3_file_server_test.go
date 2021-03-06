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
package base

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"gitlab.com/pantacor/pantahub-base/objects"
)

type mockUploader struct {
	mock.Mock
}

func (m mockUploader) Upload(key string, r io.ReadSeeker) error {
	return m.Called(key).Error(0)
}

type mockDownloader struct {
	mock.Mock
	ContentToWrite []byte
}

func (m mockDownloader) Download(key string, w io.WriterAt) error {
	if len(m.ContentToWrite) > 0 {
		w.WriteAt(m.ContentToWrite, 0)
	}
	return m.Called(key, w).Error(0)
}

type S3FileServerTestSuite struct {
	suite.Suite
	server *S3FileServer
}

func (suite *S3FileServerTestSuite) SetupTest() {
	suite.server = NewS3FileServer()
}

func (suite *S3FileServerTestSuite) generateSignedToken(method string, content []byte) string {
	sha := fmt.Sprintf("%x", sha256.Sum256(content))
	o := objects.NewObjectAccessForSec(
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

func (suite *S3FileServerTestSuite) TestPushObjectMatchingSHA() {
	content := []byte("testing")
	token := suite.generateSignedToken(http.MethodPut, content)
	req, err := http.NewRequest(http.MethodPut, "/local-s3/"+token, bytes.NewReader(content))
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusOK, rr.Code)
}

func (suite *S3FileServerTestSuite) TestPushObjectNotMatchingSHA() {
	correct := []byte("content")
	invalid := []byte("invalid-content")
	token := suite.generateSignedToken(http.MethodPut, correct)
	req, err := http.NewRequest(http.MethodPut, "/local-s3/"+token, bytes.NewReader(invalid))
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusBadRequest, rr.Code)
}

func (suite *S3FileServerTestSuite) TestGetObjectNotUploaded() {
	token := suite.generateSignedToken(http.MethodPut, nil)
	req, err := http.NewRequest(http.MethodGet, "/local-s3/"+token, nil)
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusForbidden, rr.Code)
}

func (suite *S3FileServerTestSuite) TestGetObjectWithInvalidToken() {
	req, err := http.NewRequest(http.MethodGet, "/local-s3/invalid-token", nil)
	assert.NoError(suite.T(), err)

	rr := httptest.NewRecorder()
	suite.server.ServeHTTP(rr, req)
	assert.Equal(suite.T(), http.StatusForbidden, rr.Code)
}

func TestS3FileServerTestSuite(t *testing.T) {
	suite.Run(t, new(S3FileServerTestSuite))
}
