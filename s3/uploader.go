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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type inputS3Uploader interface {
	Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error)
}

type S3Uploader interface {
	Upload(key string, r io.ReadSeeker) error
}

func (s *s3impl) Upload(key string, r io.ReadSeeker) error {
	// reset reader to start of stream
	r.Seek(0, 0)

	input := &s3manager.UploadInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Key:    aws.String(key),
		Body:   r,
	}

	_, err := s.uploader.Upload(input)
	return err
}
