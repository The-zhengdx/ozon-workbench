package storage

import (
	"fmt"
	"os"
	"strings"
)

// Config 是 OSS 运行配置。默认 Provider 为空，等于不上传。
type Config struct {
	Provider      string
	PublicBase    string
	Endpoint      string
	Region        string
	Bucket        string
	AccessKey     string
	SecretKey     string
	YandexToken   string
	YandexRoot    string
	YandexAPIBase string
}

func ConfigFromEnv() Config {
	return Config{
		Provider:      strings.TrimSpace(os.Getenv("OSS_PROVIDER")),
		PublicBase:    strings.TrimSpace(os.Getenv("OSS_PUBLIC_BASE_URL")),
		Endpoint:      strings.TrimSpace(os.Getenv("OSS_ENDPOINT")),
		Region:        strings.TrimSpace(os.Getenv("OSS_REGION")),
		Bucket:        strings.TrimSpace(os.Getenv("OSS_BUCKET")),
		AccessKey:     strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY")),
		SecretKey:     strings.TrimSpace(os.Getenv("OSS_SECRET_KEY")),
		YandexToken:   strings.TrimSpace(os.Getenv("YANDEX_DISK_TOKEN")),
		YandexRoot:    strings.TrimSpace(os.Getenv("YANDEX_DISK_ROOT")),
		YandexAPIBase: strings.TrimSpace(os.Getenv("YANDEX_DISK_API")),
	}
}

// FromConfig 选择上传适配器。none、空、以及缺凭证的厂商都不会在启动时失败。
func FromConfig(cfg Config) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "none":
		return None{}, nil
	case "mock":
		return NewMock(cfg.PublicBase), nil
	case "yandex":
		if strings.TrimSpace(cfg.YandexToken) == "" {
			return Disabled{Provider: "yandex", Reason: "已选择 Yandex Disk，但令牌为空。"}, nil
		}
		return NewYandex(cfg.YandexToken, cfg.YandexRoot, cfg.YandexAPIBase), nil
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
