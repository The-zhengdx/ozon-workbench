package storage

import (
	"fmt"
	"strings"

	"ozon-workbench/internal/conf"
)

// Open selects the provider. An empty or "none" provider is the default and does not fail startup.
// "yandex" without a token also stays disabled so the process can boot before the account exists.
func Open(cfg *conf.Runtime) (Store, error) {
	if cfg == nil {
		return None{}, nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.StorageProvider)) {
	case "", "none":
		return None{}, nil
	case "mock":
		return NewMock(cfg.StoragePublicBase), nil
	case "yandex":
		if strings.TrimSpace(cfg.YandexToken) == "" {
			return Disabled{
				Provider: "yandex",
				Reason:   "已选择 Yandex Disk，但令牌为空。请设置环境变量 YANDEX_DISK_TOKEN（不要把令牌写进仓库），然后再确认媒体。",
			}, nil
		}
		return NewYandex(cfg.YandexToken, cfg.YandexRoot, cfg.YandexAPIBase), nil
	default:
		return nil, fmt.Errorf("未知的 storage.provider %q。目前支持 none、mock、yandex", cfg.StorageProvider)
	}
}
