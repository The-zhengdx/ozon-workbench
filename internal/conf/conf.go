package conf

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the process configuration. Secrets come from the environment.
type Config struct {
	Server   Server   `yaml:"server"`
	Data     Data     `yaml:"data"`
	Ozon     Ozon     `yaml:"ozon"`
	Security Security `yaml:"security"`
}

type Server struct {
	HTTPAddr string `yaml:"http_addr"`
	Timeout  string `yaml:"timeout"`
}

type Data struct {
	Database Database `yaml:"database"`
}

type Database struct {
	DSN string `yaml:"dsn"`
}

type Ozon struct {
	BaseURL            string `yaml:"base_url"`
	Mock               bool   `yaml:"mock"`
	Timeout            string `yaml:"timeout"`
	RateLimitPerSecond int    `yaml:"rate_limit_per_second"`
	// Proxy is "", direct, env, or an http/https/socks5 URL. See NormalizeOzonProxy.
	Proxy string `yaml:"proxy"`
}

type Security struct {
	CredentialKey string `yaml:"credential_key"`
}

// Runtime is the parsed configuration used by the process.
type Runtime struct {
	HTTPAddr      string
	HTTPTimeout   time.Duration
	DatabaseDSN   string
	OzonBaseURL   string
	OzonMock      bool
	OzonTimeout   time.Duration
	OzonRateLimit int
	OzonProxy     string
	CredentialKey string
}

// Load reads YAML then applies environment overrides.
func Load(path string) (*Runtime, error) {
	cfg := &Config{}
	cfg.Server.HTTPAddr = "0.0.0.0:18081"
	cfg.Server.Timeout = "60s"
	cfg.Ozon.BaseURL = "https://api-seller.ozon.ru"
	cfg.Ozon.Mock = true
	cfg.Ozon.Timeout = "45s"
	cfg.Ozon.RateLimitPerSecond = 10
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取配置 %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("解析配置: %w", err)
		}
	}
	if v := strings.TrimSpace(os.Getenv("HTTP_ADDR")); v != "" {
		cfg.Server.HTTPAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		cfg.Data.Database.DSN = v
	}
	if v := strings.TrimSpace(os.Getenv("OZON_API_BASE")); v != "" {
		cfg.Ozon.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("OZON_MOCK")); v != "" {
		cfg.Ozon.Mock = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := strings.TrimSpace(os.Getenv("OZON_RATE_LIMIT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("OZON_RATE_LIMIT: %w", err)
		}
		cfg.Ozon.RateLimitPerSecond = n
	}
	if v := strings.TrimSpace(os.Getenv("OZON_PROXY")); v != "" {
		cfg.Ozon.Proxy = v
	}
	if v := os.Getenv("OZON_CREDENTIAL_KEY"); strings.TrimSpace(v) != "" {
		cfg.Security.CredentialKey = v
	}
	if strings.TrimSpace(cfg.Data.Database.DSN) == "" {
		return nil, fmt.Errorf("未配置数据库连接，请设置 data.database.dsn 或 DATABASE_URL")
	}
	httpTimeout, err := time.ParseDuration(defaultString(cfg.Server.Timeout, "60s"))
	if err != nil {
		return nil, fmt.Errorf("server.timeout: %w", err)
	}
	ozonTimeout, err := time.ParseDuration(defaultString(cfg.Ozon.Timeout, "45s"))
	if err != nil {
		return nil, fmt.Errorf("ozon.timeout: %w", err)
	}
	if cfg.Ozon.RateLimitPerSecond <= 0 {
		cfg.Ozon.RateLimitPerSecond = 10
	}
	if strings.TrimSpace(cfg.Ozon.BaseURL) == "" {
		cfg.Ozon.BaseURL = "https://api-seller.ozon.ru"
	}
	proxy, err := NormalizeOzonProxy(cfg.Ozon.Proxy)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		HTTPAddr:      cfg.Server.HTTPAddr,
		HTTPTimeout:   httpTimeout,
		DatabaseDSN:   cfg.Data.Database.DSN,
		OzonBaseURL:   strings.TrimRight(cfg.Ozon.BaseURL, "/"),
		OzonMock:      cfg.Ozon.Mock,
		OzonTimeout:   ozonTimeout,
		OzonRateLimit: cfg.Ozon.RateLimitPerSecond,
		OzonProxy:     proxy,
		CredentialKey: cfg.Security.CredentialKey,
	}, nil
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
