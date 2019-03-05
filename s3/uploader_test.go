//
// Copyright 2019 Pantacor Ltd.
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
package s3

import (
	"bytes"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type mockInputS3Uploader struct {
	mock.Mock
}

func (m mockInputS3Uploader) Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error) {
	args := m.Called(input, options)
	output := args.Get(0)
	if output == nil {
		return nil, args.Error(1)
	}

	return output.(*s3manager.UploadOutput), args.Error(1)
}

type S3UploaderTestSuite struct {
	suite.Suite
}

func (suite S3UploaderTestSuite) TestUploadWithSuccessfullUpload() {
	key := "testkey"
	content := bytes.NewReader([]byte("testcontent"))

	expectedInput := &s3manager.UploadInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String("testkey"),
		Body:   content,
	}
	inputUploader := mockInputS3Uploader{}
	inputUploader.On("Upload", expectedInput, mock.AnythingOfType("[]func(*s3manager.Uploader)")).Return(nil, nil)

	params := S3ConnectionParameters{
		Bucket: bucket,
	}
	uploader := &s3impl{
		uploader:         inputUploader,
		connectionParams: params,
	}

	err := uploader.Upload(key, content)
	assert.NoError(suite.T(), err)
}

func (suite S3UploaderTestSuite) TestUploadWithError() {
	key := "testkey"
	content := bytes.NewReader([]byte("testcontent"))

	expectedInput := &s3manager.UploadInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String("testkey"),
		Body:   content,
	}
	inputUploader := mockInputS3Uploader{}
	inputUploader.On("Upload", expectedInput, mock.AnythingOfType("[]func(*s3manager.Uploader)")).Return(nil, assert.AnError)

	params := S3ConnectionParameters{
		Bucket: bucket,
	}
	uploader := &s3impl{
		uploader:         inputUploader,
		connectionParams: params,
	}

	err := uploader.Upload(key, content)
	assert.Error(suite.T(), err)
}

func (suite S3UploaderTestSuite) TestUploadIncompleteData() {
	key := "testkey"
	content := bytes.NewReader([]byte(make([]byte, 1024*1024*1024*10)))

	expectedInput := &s3manager.UploadInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String("testkey"),
		Body:   content,
	}
	inputUploader := mockInputS3Uploader{}
	inputUploader.On("Upload", expectedInput, mock.AnythingOfType("[]func(*s3manager.Uploader)")).Return(nil, assert.AnError)

	params := S3ConnectionParameters{
		Bucket: testBucket,
	}
	uploader := &s3impl{
		uploader:         inputUploader,
		connectionParams: params,
	}

	err := uploader.Upload(key, content)
	assert.Error(suite.T(), err)
}

func TestS3UploaderTestSuite(t *testing.T) {
	suite.Run(t, new(S3UploaderTestSuite))
}
