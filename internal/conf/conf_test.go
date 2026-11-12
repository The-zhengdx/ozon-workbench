package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeOzonProxy(t *testing.T) {
	for _, raw := range []string{"", "direct", "OFF", " none "} {
		got, err := NormalizeOzonProxy(raw)
		if err != nil || got != "direct" {
			t.Fatalf("%q -> %q %v", raw, got, err)
		}
	}
	got, err := NormalizeOzonProxy("ENV")
	if err != nil || got != "env" {
		t.Fatalf("env %q %v", got, err)
	}
	got, err = NormalizeOzonProxy("http://user:secret@127.0.0.1:7890")
	if err != nil || got != "http://user:secret@127.0.0.1:7890" {
		t.Fatalf("url %q %v", got, err)
	}
	if _, err := NormalizeOzonProxy("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("ftp should be rejected")
	}
	if _, err := NormalizeOzonProxy("127.0.0.1:7890"); err == nil {
		t.Fatal("missing scheme should be rejected")
	}
}

func TestLoadProxyAndMockOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("data:\n  database:\n    dsn: postgres://ozon:ozon@127.0.0.1:5432/ozon_workbench?sslmode=disable\nozon:\n  mock: true\n  proxy: http://127.0.0.1:1\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("OZON_MOCK", "")
	t.Setenv("OZON_PROXY", "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OzonMock || cfg.OzonProxy != "http://127.0.0.1:1" {
		t.Fatalf("yaml %#v", cfg)
	}
	t.Setenv("OZON_MOCK", "false")
	t.Setenv("OZON_PROXY", "direct")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OzonMock || cfg.OzonProxy != "direct" {
		t.Fatalf("env override mock=%v proxy=%s", cfg.OzonMock, cfg.OzonProxy)
	}
}
