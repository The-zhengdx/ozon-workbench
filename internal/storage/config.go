package storage

import (
	"fmt"
	"strings"
)

// Config 选择进程内的上传适配器。正式环境不读环境变量，S3 配置在系统设置里。
type Config struct {
	Provider   string
	PublicBase string
	Endpoint   string
	Region     string
	Bucket     string
	AccessKey  string
	SecretKey  string
}

// FromConfig 选择上传适配器。空配置等于不上传，启动时不报错。
func FromConfig(cfg Config) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "none":
		return None{}, nil
	case "mock":
		return NewMock(cfg.PublicBase), nil
	case "s3":
		if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.PublicBase == "" {
			return Disabled{Provider: "s3", Reason: "S3 需要 Endpoint、Bucket、Access Key、Secret Key 和长期公开基址。"}, nil
		}
		if !strings.HasPrefix(cfg.PublicBase, "https://") {
			return nil, fmt.Errorf("OSS 公开基址必须是 HTTPS")
		}
		return NewS3(S3Config{
			Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket,
			AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey, PublicBase: cfg.PublicBase,
		}), nil
	default:
		return nil, fmt.Errorf("未知的 OSS Provider %q", cfg.Provider)
	}
}
