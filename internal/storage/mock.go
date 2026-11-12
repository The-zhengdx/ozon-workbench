package storage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Mock keeps files in memory and returns HTTPS URLs under Base.
// Those URLs are for tests and the local Ozon mock. Ozon's real image fetcher cannot download them.
type Mock struct {
	Base     string
	FailLeft int
	mu       sync.Mutex
	files    map[string]Object
}

func NewMock(base string) *Mock {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://mock-storage.local"
	}
	return &Mock{Base: base, files: map[string]Object{}}
}

func (m *Mock) Name() string     { return "mock" }
func (m *Mock) Configured() bool { return true }
func (m *Mock) Message() string {
	return "模拟图床已启用。返回的 HTTPS 地址只供本地测试，Ozon 正式环境拉取不到。"
}
func (m *Mock) Health(context.Context) error { return nil }

func (m *Mock) Upload(_ context.Context, localPath, key string) (Object, error) {
	key = strings.Trim(key, "/")
	if key == "" || strings.Contains(key, "..") {
		return Object{}, fmt.Errorf("非法的对象键")
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return Object{}, fmt.Errorf("读取本地文件: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailLeft > 0 {
		m.FailLeft--
		return Object{}, fmt.Errorf("模拟图床暂时失败")
	}
	obj := Object{
		Key:       key,
		PublicURL: m.Base + "/" + key,
		PublicKey: "mock:" + key,
		Size:      info.Size(),
		MIME:      "application/octet-stream",
	}
	m.files[key] = obj
	return obj, nil
}

func (m *Mock) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, strings.Trim(key, "/"))
	return nil
}

func (m *Mock) Refresh(_ context.Context, publicKey string) (string, error) {
	key := strings.TrimPrefix(publicKey, "mock:")
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.files[key]
	if !ok {
		return "", fmt.Errorf("模拟图床里没有这个文件")
	}
	return obj.PublicURL, nil
}
