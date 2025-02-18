package storage

import (
    "context"
    "io"
)

type Storage interface {
    UploadFile(ctx context.Context, bucketName, objectKey string, reader io.Reader, storageClass string) error
}