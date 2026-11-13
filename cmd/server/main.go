package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ozon-workbench/internal/app"
	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/envfile"
	"ozon-workbench/internal/httpapi"
	"ozon-workbench/internal/ozonapi"
	"ozon-workbench/internal/storage"
	"ozon-workbench/internal/store"
)

func main() {
	envfile.Load()
	addr := getenv("HTTP_ADDR", "0.0.0.0:18081")
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		log.Fatal("缺少 DATABASE_URL。请在 .env 里配置 PostgreSQL 连接串。")
	}
	ctx := context.Background()
	db, err := store.OpenPostgres(ctx, dbURL)
	if err != nil {
		log.Fatalf("连接 PostgreSQL 失败: %v", err)
	}
	defer db.Close()
	migrations := getenv("MIGRATIONS_DIR", "migrations")
	if err := store.Migrate(ctx, db.Pool(), migrations); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}
	mockOzon := strings.EqualFold(os.Getenv("OZON_MOCK"), "true")
	base := getenv("OZON_API_BASE", "https://api-seller.ozon.ru")
	engine := &app.Engine{
		DB:       db,
		Codex:    codexcli.CLI{},
		WorkRoot: getenv("WORK_ROOT", filepath.Join("data", "work")),
		APIBase:  "http://" + strings.Replace(addr, "0.0.0.0", "127.0.0.1", 1),
		Ozon: func(shop domain.Shop) ozonapi.Client {
			if mockOzon {
				return &ozonapi.Mock{Currency: shop.CurrencyCode, TreeBody: []byte(`[]`)}
			}
			return ozonapi.NewHTTP(base, shop.ClientID, shop.APIKey)
		},
		Uploader: func() storage.Store {
			store, err := storage.FromConfig(storage.ConfigFromEnv())
			if err != nil {
				log.Printf("OSS 配置无效: %v", err)
				return storage.None{}
			}
			return store
		},
	}
	if err := engine.Recover(ctx); err != nil {
		log.Printf("启动巡检: %v", err)
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := engine.Pump(context.Background()); err != nil {
				log.Printf("调度: %v", err)
			}
		}
	}()
	server := &http.Server{Addr: addr, Handler: httpapi.New(engine).Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Ozon 工作台监听 %s（Ozon mock=%v）", addr, mockOzon)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
