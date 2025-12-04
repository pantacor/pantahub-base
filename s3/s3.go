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
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// S3 application interface
type S3 interface {
	Exists(ctx context.Context, key string) bool
	Delete(ctx context.Context, key string) error
	Rename(ctx context.Context, oldKey, newKey string) error
	UploadURL(ctx context.Context, key string) (string, error)
	DownloadURL(ctx context.Context, key string) (string, error)
	GetConnectionParams(ctx context.Context) ConnectionParameters
}

type s3impl struct {
	connectionParams ConnectionParameters
	session          *s3.Client
	presignClient    *s3.PresignClient
}

func (s *s3impl) GetConnectionParams(ctx context.Context) ConnectionParameters {
	return s.connectionParams
}

func (s *s3impl) Delete(ctx context.Context, key string) error {
	deleteInput := &s3.DeleteObjectInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Key:    aws.String(key),
	}

	_, err := s.session.DeleteObject(ctx, deleteInput)
	return err
}

func (s *s3impl) Rename(ctx context.Context, oldKey, newKey string) error {
	copyInput := &s3.CopyObjectInput{
		Bucket:     aws.String(s.connectionParams.Bucket),
		CopySource: aws.String(s.connectionParams.Bucket + "/" + oldKey),
		Key:        aws.String(newKey),
	}

	_, err := s.session.CopyObject(ctx, copyInput)
	if err != nil {
		return err
	}

	s.Delete(ctx, oldKey)
	return nil
}

func (s *s3impl) Exists(ctx context.Context, key string) bool {
	if key == "" {
		return false
	}

	// Check for nil session
	if s.session == nil {
		return false
	}

	if string(key[0]) == `/` {
		key = key[1:]
	}

	// Check for empty bucket name
	if s.connectionParams.Bucket == "" {
		return false
	}

	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Key:    aws.String(key),
	}

	head, err := s.session.HeadObject(ctx, input)
	if err != nil {
		var aerr smithy.APIError
		if errors.As(err, &aerr) {
			// Log the full error details for debugging
			switch aerr.ErrorCode() {
			case "NotFound", "BadRequest", "NoSuchBucket", "NoSuchKey", "Forbidden":
				return false
			default:
				fmt.Printf("Unexpected error checking if object exists: %v\n", aerr)
				return false
			}
		}
		// Log non-API errors
		fmt.Printf("Non-API error checking if object exists: %v\n", err)
		return false
	}

	// Verify we have a valid head response
	if head == nil {
		return false
	}

	return head.ContentLength != nil && *head.ContentLength >= 0
}

// New create a new S3 application
func New(ctx context.Context, params ConnectionParameters) S3 {
	if !params.IsValid() {
		return nil
	}
	creds := credentials.NewStaticCredentialsProvider(params.AccessKey, params.SecretKey, "")
	cfg, err := config.LoadDefaultConfig(ctx,
		// config.WithClientLogMode(aws.LogRequest),
		config.WithRegion(params.Region),
		config.WithCredentialsProvider(creds),
	)

	if err != nil {
		fmt.Printf("error loading aws configuration -- %s\n", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(params.Endpoint)
		o.UsePathStyle = true
	})

	presignClient := s3.NewPresignClient(client)

	return &s3impl{
		session:          client,
		presignClient:    presignClient,
		connectionParams: params,
	}
}
