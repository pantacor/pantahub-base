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
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Uploader Uploader interface
type Uploader interface {
	UploadURL(ctx context.Context, key string) (string, error)
}

func (s *s3impl) UploadURL(ctx context.Context, key string) (string, error) {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.connectionParams.Bucket),
		Key:    aws.String(key),
	}

	presignURL, err := s.presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(60*time.Minute))
	if err != nil {
		return "", fmt.Errorf("failed to presign GetObject request: %v", err)
	}
	return presignURL.URL, nil
}
