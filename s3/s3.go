package s3

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

type S3 interface {
	Delete(key string) error
	Rename(oldKey, newKey string) error
	UploadURL(key string) (string, error)
	DownloadURL(key string) (string, error)
}

type s3impl struct {
	connectionParams S3ConnectionParameters
	session          *s3.S3
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

func NewS3(params S3ConnectionParameters) S3 {
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
