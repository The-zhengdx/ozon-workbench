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
	Media    Media    `yaml:"media"`
	Codex    Codex    `yaml:"codex"`
	Storage  Storage  `yaml:"storage"`
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

// Media is the local directory that holds extracted 1688 packages.
type Media struct {
	Root                 string `yaml:"root"`
	MaxZipBytes          int64  `yaml:"max_zip_bytes"`
	MaxUncompressedBytes int64  `yaml:"max_uncompressed_bytes"`
	MaxFileCount         int    `yaml:"max_file_count"`
}

// Storage selects the public media host. Provider is empty or "none" until the operator configures one.
// The Yandex token is read from the environment so it is never committed.
type Storage struct {
	Provider string `yaml:"provider"`
	Yandex   Yandex `yaml:"yandex"`
	// PublicBase is only used by the mock provider.
	PublicBase string `yaml:"public_base"`
}

type Yandex struct {
	Token   string `yaml:"token"`
	Root    string `yaml:"root"`
	APIBase string `yaml:"api_base"`
}

// Codex configures the local Codex CLI. Mock is the default for development and CI.
type Codex struct {
	Mock       bool     `yaml:"mock"`
	Binary     string   `yaml:"binary"`
	Timeout    string   `yaml:"timeout"`
	Workers    int      `yaml:"workers"`
	NewArgs    []string `yaml:"new_args"`
	ResumeArgs []string `yaml:"resume_args"`
}

// Runtime is the parsed configuration used by the process.
type Runtime struct {
	HTTPAddr             string
	HTTPTimeout          time.Duration
	DatabaseDSN          string
	OzonBaseURL          string
	OzonMock             bool
	OzonTimeout          time.Duration
	OzonRateLimit        int
	OzonProxy            string
	CredentialKey        string
	MediaRoot            string
	MaxZipBytes          int64
	MaxUncompressedBytes int64
	MaxFileCount         int
	CodexMock            bool
	CodexBinary          string
	CodexTimeout         time.Duration
	CodexWorkers         int
	CodexNewArgs         []string
	CodexResumeArgs      []string
	StorageProvider      string
	StoragePublicBase    string
	YandexToken          string
	YandexRoot           string
	YandexAPIBase        string
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
	cfg.Media.Root = "data/media"
	cfg.Media.MaxZipBytes = 200 << 20
	cfg.Media.MaxUncompressedBytes = 500 << 20
	cfg.Media.MaxFileCount = 4000
	cfg.Codex.Mock = true
	cfg.Codex.Binary = "codex"
	cfg.Codex.Timeout = "15m"
	cfg.Codex.Workers = 2
	cfg.Codex.NewArgs = defaultCodexNewArgs()
	cfg.Codex.ResumeArgs = defaultCodexResumeArgs()
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
	if v := strings.TrimSpace(os.Getenv("MEDIA_ROOT")); v != "" {
		cfg.Media.Root = v
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_MOCK")); v != "" {
		cfg.Codex.Mock = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_BIN")); v != "" {
		cfg.Codex.Binary = v
	}
	if v := strings.TrimSpace(os.Getenv("CODEX_TIMEOUT")); v != "" {
		cfg.Codex.Timeout = v
	}
	if v, ok := os.LookupEnv("STORAGE_PROVIDER"); ok {
		cfg.Storage.Provider = strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(os.Getenv("YANDEX_DISK_TOKEN")); v != "" {
		cfg.Storage.Yandex.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("YANDEX_DISK_ROOT")); v != "" {
		cfg.Storage.Yandex.Root = v
	}
	if v := strings.TrimSpace(os.Getenv("YANDEX_DISK_API")); v != "" {
		cfg.Storage.Yandex.APIBase = v
	}
	if v := strings.TrimSpace(os.Getenv("STORAGE_PUBLIC_BASE")); v != "" {
		cfg.Storage.PublicBase = v
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
	codexTimeout, err := time.ParseDuration(defaultString(cfg.Codex.Timeout, "15m"))
	if err != nil {
		return nil, fmt.Errorf("codex.timeout: %w", err)
	}
	if strings.TrimSpace(cfg.Media.Root) == "" {
		cfg.Media.Root = "data/media"
	}
	if cfg.Media.MaxZipBytes <= 0 {
		cfg.Media.MaxZipBytes = 200 << 20
	}
	if cfg.Media.MaxUncompressedBytes <= 0 {
		cfg.Media.MaxUncompressedBytes = 500 << 20
	}
	if cfg.Media.MaxFileCount <= 0 {
		cfg.Media.MaxFileCount = 4000
	}
	if strings.TrimSpace(cfg.Codex.Binary) == "" {
		cfg.Codex.Binary = "codex"
	}
	if cfg.Codex.Workers <= 0 {
		cfg.Codex.Workers = 2
	}
	if len(cfg.Codex.NewArgs) == 0 {
		cfg.Codex.NewArgs = defaultCodexNewArgs()
	}
	if len(cfg.Codex.ResumeArgs) == 0 {
		cfg.Codex.ResumeArgs = defaultCodexResumeArgs()
	}
	return &Runtime{
		HTTPAddr:             cfg.Server.HTTPAddr,
		HTTPTimeout:          httpTimeout,
		DatabaseDSN:          cfg.Data.Database.DSN,
		OzonBaseURL:          strings.TrimRight(cfg.Ozon.BaseURL, "/"),
		OzonMock:             cfg.Ozon.Mock,
		OzonTimeout:          ozonTimeout,
		OzonRateLimit:        cfg.Ozon.RateLimitPerSecond,
		OzonProxy:            proxy,
		CredentialKey:        cfg.Security.CredentialKey,
		MediaRoot:            cfg.Media.Root,
		MaxZipBytes:          cfg.Media.MaxZipBytes,
		MaxUncompressedBytes: cfg.Media.MaxUncompressedBytes,
		MaxFileCount:         cfg.Media.MaxFileCount,
		CodexMock:            cfg.Codex.Mock,
		CodexBinary:          cfg.Codex.Binary,
		CodexTimeout:         codexTimeout,
		CodexWorkers:         cfg.Codex.Workers,
		CodexNewArgs:         cfg.Codex.NewArgs,
		CodexResumeArgs:      cfg.Codex.ResumeArgs,
		StorageProvider:      strings.ToLower(strings.TrimSpace(cfg.Storage.Provider)),
		StoragePublicBase:    strings.TrimSpace(cfg.Storage.PublicBase),
		YandexToken:          strings.TrimSpace(cfg.Storage.Yandex.Token),
		YandexRoot:           strings.Trim(strings.TrimSpace(cfg.Storage.Yandex.Root), "/"),
		YandexAPIBase:        strings.TrimRight(strings.TrimSpace(cfg.Storage.Yandex.APIBase), "/"),
	}, nil
}

func parseBool(v string) bool {
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// defaultCodexNewArgs matches current Codex CLI non-interactive exec:
// codex exec --json --skip-git-repo-check --sandbox workspace-write -C <dir> -o <file> -
// The trailing "-" reads the prompt from stdin.
// https://learn.chatgpt.com/docs/non-interactive-mode
func defaultCodexNewArgs() []string {
	return []string{
		"exec", "--json", "--skip-git-repo-check", "--color", "never",
		"--sandbox", "workspace-write",
		"-C", "{{work_dir}}",
		"-o", "{{last_message}}",
		"-",
	}
}

// defaultCodexResumeArgs continues the same session:
// codex exec resume <SESSION_ID> --json ... -
func defaultCodexResumeArgs() []string {
	return []string{
		"exec", "resume", "{{session_id}}",
		"--json", "--skip-git-repo-check", "--color", "never",
		"--sandbox", "workspace-write",
		"-C", "{{work_dir}}",
		"-o", "{{last_message}}",
		"-",
	}
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
