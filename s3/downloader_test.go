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
	"io"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type mockInputS3Downloader struct {
	mock.Mock
}

func (m mockInputS3Downloader) Download(w io.WriterAt, input *s3.GetObjectInput, options ...func(*s3manager.Downloader)) (n int64, err error) {
	args := m.Called(w, input, options)
	output := args.Get(0)
	if output == nil {
		return 0, args.Error(1)
	}

	return output.(int64), args.Error(1)
}

type mockWriterAt struct {
	mock.Mock
}

func (m *mockWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	args := m.Called(p, off)
	return args.Int(0), args.Error(1)
}

type S3DownloaderTestSuite struct {
	suite.Suite
}

func (suite S3DownloaderTestSuite) TestDownloadWithSuccessfullDownload() {
	key := "testkey"
	bucket := "testbucket"

	expectedInput := &s3.GetObjectInput{
		Bucket: aws.String("testbucket"),
		Key:    aws.String("testkey"),
	}
	inputDownloader := mockInputS3Downloader{}
	inputDownloader.On("Download", mock.Anything, expectedInput, mock.Anything).Return(nil, nil)

	params := ConnectionParameters{
		Bucket: bucket,
	}
	downloader := &s3impl{
		downloader:       inputDownloader,
		connectionParams: params,
	}

	err := downloader.Download(key, &mockWriterAt{})
	assert.NoError(suite.T(), err)

}

func (suite S3DownloaderTestSuite) TestDownloadWithError() {
	key := "testkey"
	bucket := "testbucket"

	expectedInput := &s3.GetObjectInput{
		Bucket: aws.String("testbucket"),
		Key:    aws.String("testkey"),
	}
	inputDownloader := mockInputS3Downloader{}
	inputDownloader.On("Download", mock.Anything, expectedInput, mock.Anything).Return(nil, assert.AnError)

	params := ConnectionParameters{
		Bucket: bucket,
	}
	downloader := &s3impl{
		downloader:       inputDownloader,
		connectionParams: params,
	}

	err := downloader.Download(key, &mockWriterAt{})
	assert.Error(suite.T(), err)
}

func TestS3DownloaderTestSuite(t *testing.T) {
	suite.Run(t, new(S3DownloaderTestSuite))
}
