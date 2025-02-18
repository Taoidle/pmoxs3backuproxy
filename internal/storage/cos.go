package storage

import (
	"context"
	"github.com/tencentyun/cos-go-sdk-v5"
	"io"
	"net/http"
	"net/url"
)

type COSStorage struct {
	client *cos.Client
}

func NewCOSStorage(secretID, secretKey, bucketURL string) (*COSStorage, error) {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return nil, err
	}
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  secretID,
			SecretKey: secretKey,
		},
	})
	return &COSStorage{client: c}, nil
}

func (c *COSStorage) UploadFile(ctx context.Context, bucketName, objectKey string, reader io.Reader, storageClass string) error {
	mappedStorageClass := GetStorageClass("tencent", storageClass)
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			XCosStorageClass: mappedStorageClass,
		},
	}
	_, err := c.client.Object.Put(ctx, objectKey, reader, opt)
	return err
}
