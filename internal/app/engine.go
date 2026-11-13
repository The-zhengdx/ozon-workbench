package app

import (
	"context"
	"sync"
	"time"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/ozonapi"
	"ozon-workbench/internal/storage"
	"ozon-workbench/internal/store"
)

// Engine 实现店铺、类目、加工、交付和刊登。外部依赖都从这里注入。
type Engine struct {
	DB             store.DB
	Codex          codexcli.Runner
	Ozon           func(domain.Shop) ozonapi.Client
	Uploader       func() storage.Store
	WorkRoot       string
	APIBase        string
	Now            func() time.Time
	BatchLimit     int
	AllowTestMedia bool
	VerifyURL      func(ctx context.Context, rawURL string) error

	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	inflight map[string]chan struct{}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) batchLimit() int {
	if e.BatchLimit > 0 {
		return e.BatchLimit
	}
	return 100
}

func (e *Engine) apiBase() string {
	if e.APIBase != "" {
		return e.APIBase
	}
	return "http://127.0.0.1:18081"
}

func (e *Engine) uploader() storage.Store {
	if e.Uploader == nil {
		return storage.None{}
	}
	store := e.Uploader()
	if store == nil {
		return storage.None{}
	}
	return store
}

func (e *Engine) ozon(shop domain.Shop) ozonapi.Client {
	if e.Ozon == nil {
		return &ozonapi.Mock{}
	}
	client := e.Ozon(shop)
	if client == nil {
		return &ozonapi.Mock{}
	}
	return client
}

func (e *Engine) once(key string, fn func() error) error {
	e.mu.Lock()
	if e.inflight == nil {
		e.inflight = map[string]chan struct{}{}
	}
	if ch, ok := e.inflight[key]; ok {
		e.mu.Unlock()
		<-ch
		return nil
	}
	ch := make(chan struct{})
	e.inflight[key] = ch
	e.mu.Unlock()
	err := fn()
	close(ch)
	e.mu.Lock()
	delete(e.inflight, key)
	e.mu.Unlock()
	return err
}
