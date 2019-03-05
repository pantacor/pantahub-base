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
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  //   See the License for the specific language governing permissions and
//   limitations under the License.
//
package s3

import (
	"io"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type inputS3Downloader interface {
	Download(w io.WriterAt, input *s3.GetObjectInput, options ...func(*s3manager.Downloader)) (n int64, err error)
}

type S3Downloader interface {
	Download(key string, w io.WriterAt) error
}

func (s *s3impl) Download(key string, w io.WriterAt) error {
	log.Printf("INFO: downloading file %s using parameters %+v\n", key, s.connectionParams)
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Key:    aws.String(key),
	}

	_, err := s.downloader.Download(w, input)
	return err
}
