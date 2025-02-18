package storage

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"io"
)

type S3Storage struct {
	client *s3.Client
}

func NewS3Storage() (*S3Storage, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg)
	return &S3Storage{client: client}, nil
}

func (s *S3Storage) UploadFile(ctx context.Context, bucketName, objectKey string, reader io.Reader, storageClass string) error {
	input := &s3.PutObjectInput{
		Bucket:       &bucketName,
		Key:          &objectKey,
		Body:         reader,
		StorageClass: types.StorageClass(storageClass),
	}
	_, err := s.client.PutObject(ctx, input)
	return err
}
