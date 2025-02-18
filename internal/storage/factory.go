package storage

import (
	"errors"
)

type StorageFactory struct {
	Config *StorageConfig
}

func NewStorageFactory(config *StorageConfig) *StorageFactory {
	return &StorageFactory{Config: config}
}

func (f *StorageFactory) GetStorage(provider string) (Storage, error) {
	switch provider {
	case "aws":
		return NewS3Storage()
	case "tencent":
		return NewCOSStorage(f.Config.COSSecretID, f.Config.COSSecretKey, f.Config.COSBucketURL)
	case "alibaba":
		return NewOSSStorage(f.Config.OSSEndpoint, f.Config.OSSAccessKeyID, f.Config.OSSAccessKeySecret, f.Config.OSSBucketName)
	default:
		return nil, errors.New("unsupported cloud provider")
	}
}
