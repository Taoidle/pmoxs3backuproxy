package storage

type StorageConfig struct {
	// AWS S3配置
	// AWS凭证将从环境变量或默认配置文件中读取

	// 腾讯云COS配置
	COSSecretID  string
	COSSecretKey string
	COSBucketURL string

	// 阿里云OSS配置
	OSSEndpoint        string
	OSSAccessKeyID     string
	OSSAccessKeySecret string
	OSSBucketName      string
}

// 存储类型映射
var (
	// S3存储类型映射
	S3StorageClassMap = map[string]string{
		"STANDARD":             "STANDARD",
		"REDUCED_REDUNDANCY":   "REDUCED_REDUNDANCY",
		"STANDARD_IA":          "STANDARD_IA",
		"ONEZONE_IA":          "ONEZONE_IA",
		"INTELLIGENT_TIERING": "INTELLIGENT_TIERING",
		"GLACIER":             "GLACIER",
		"DEEP_ARCHIVE":        "DEEP_ARCHIVE",
	}

	// 腾讯云COS存储类型映射
	COSStorageClassMap = map[string]string{
		"STANDARD":             "STANDARD",
		"STANDARD_IA":          "STANDARD_IA",
		"INTELLIGENT_TIERING": "INTELLIGENT_TIERING",
		"ARCHIVE":             "ARCHIVE",
		"DEEP_ARCHIVE":        "DEEP_ARCHIVE",
	}

	// 阿里云OSS存储类型映射
	OSSStorageClassMap = map[string]string{
		"STANDARD":      "Standard",
		"STANDARD_IA":   "IA",
		"ARCHIVE":      "Archive",
		"COLD_ARCHIVE": "ColdArchive",
		"DEEP_ARCHIVE": "DeepColdArchive",
	}
)

// GetStorageClass 根据提供商和存储类型获取对应的SDK存储类型
func GetStorageClass(provider, storageClass string) string {
	switch provider {
	case "aws":
		if class, ok := S3StorageClassMap[storageClass]; ok {
			return class
		}
	case "tencent":
		if class, ok := COSStorageClassMap[storageClass]; ok {
			return class
		}
	case "alibaba":
		if class, ok := OSSStorageClassMap[storageClass]; ok {
			return class
		}
	}
	return "STANDARD" // 默认返回标准存储类型
}