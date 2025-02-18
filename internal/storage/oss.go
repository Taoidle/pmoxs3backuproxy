package storage

import (
    "context"
    "github.com/aliyun/aliyun-oss-go-sdk/oss"
    "io"
)

type OSSStorage struct {
    client *oss.Client
    bucket *oss.Bucket
}

func NewOSSStorage(endpoint, accessKeyID, accessKeySecret, bucketName string) (*OSSStorage, error) {
    client, err := oss.New(endpoint, accessKeyID, accessKeySecret)
    if err != nil {
        return nil, err
    }
    bucket, err := client.Bucket(bucketName)
    if err != nil {
        return nil, err
    }
    return &OSSStorage{client: client, bucket: bucket}, nil
}

func (o *OSSStorage) UploadFile(ctx context.Context, bucketName, objectKey string, reader io.Reader, storageClass string) error {
    mappedStorageClass := GetStorageClass("alibaba", storageClass)
    options := []oss.Option{
        oss.StorageClass(oss.StorageClassType(mappedStorageClass)),
    }
    err := o.bucket.PutObject(objectKey, reader, options...)
    return err
}