//
// Copyright 2020 Pantacor Ltd.
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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// S3 application interface
type S3 interface {
	Exists(key string) bool
	Delete(key string) error
	Rename(oldKey, newKey string) error
	UploadURL(key string) (string, error)
	DownloadURL(key string) (string, error)
	GetConnectionParams() ConnectionParameters
}

type s3impl struct {
	connectionParams ConnectionParameters
	session          *s3.S3
}

func (s *s3impl) GetConnectionParams() ConnectionParameters {
	return s.connectionParams
}

func (s *s3impl) Delete(key string) error {
	deleteInput := &s3.DeleteObjectInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Key:    aws.String(key),
	}

	_, err := s.session.DeleteObject(deleteInput)
	return err
}

func (s *s3impl) Rename(oldKey, newKey string) error {
	copyInput := &s3.CopyObjectInput{
		Bucket:     aws.String(s.connectionParams.Bucket),
		CopySource: aws.String(s.connectionParams.Bucket + oldKey),
		Key:        aws.String(newKey),
	}

	_, err := s.session.CopyObject(copyInput)
	if err != nil {
		return err
	}

	s.Delete(oldKey)
	return nil
}

func (s *s3impl) Exists(key string) bool {
	if key == "" {
		return false
	}

	if string(key[0]) == `/` {
		key = key[1:]
	}

	listInput := &s3.ListObjectsInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Prefix: aws.String(key),
	}

	out, err := s.session.ListObjects(listInput)
	if err != nil {
		return false
	}

	return len(out.Contents) > 0
}

// New create a new S3 application
func New(params ConnectionParameters) S3 {
	if !params.IsValid() {
		return nil
	}

	awsConfig := &aws.Config{
		LogLevel:         aws.LogLevel(aws.LogDebugWithHTTPBody),
		S3ForcePathStyle: aws.Bool(true),
		Region:           aws.String(params.Region),
	}

	awsConfig.Credentials = credentials.NewStaticCredentials(
		params.AccessKey,
		params.SecretKey,
		"", //
	)

	if params.Endpoint != "" {
		awsConfig.Endpoint = aws.String(params.Endpoint)
	}

	session, err := session.NewSession(awsConfig)
	if err != nil {
		return nil
	}

	s3session := s3.New(session)

	return &s3impl{
		session:          s3session,
		connectionParams: params,
	}
}
