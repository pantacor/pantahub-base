package s3

import (
	"io"
	"runtime"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3 interface {
	Upload(key string, r io.ReadSeeker) error
	Download(key string, w io.WriterAt) error
}

type s3impl struct {
	connectionParams S3ConnectionParameters
	uploader         inputS3Uploader
	downloader       inputS3Downloader
}

func NewS3(params S3ConnectionParameters) S3 {
	if !params.IsValid() {
		return nil
	}

	awsConfig := &aws.Config{
		S3ForcePathStyle: aws.Bool(true),
		Region:           aws.String(params.Region),
	}

	if params.AnonymousCredentials {
		awsConfig.Credentials = credentials.AnonymousCredentials
	} else {
		awsConfig.Credentials = credentials.NewStaticCredentials(
			params.AccessKey,
			params.SecretKey,
			"", //
		)
	}

	if params.Endpoint != "" {
		awsConfig.Endpoint = aws.String(params.Endpoint)
	}

	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil
	}

	uploader := s3manager.NewUploader(s, func(u *s3manager.Uploader) {
		u.Concurrency = runtime.NumCPU()
		u.LeavePartsOnError = false
	})

	downloader := s3manager.NewDownloader(s, func(u *s3manager.Downloader) {
		u.Concurrency = runtime.NumCPU()
	})

	return &s3impl{
		uploader:         uploader,
		downloader:       downloader,
		connectionParams: params,
	}
}
