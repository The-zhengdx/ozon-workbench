package server

import (
	"net/http"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/conf"
	"ozon-workbench/internal/service"
)

func NewHTTPServer(cfg *conf.Runtime, shops *service.ShopService, categories *service.CategoryService, tasks *service.TaskService, logger log.Logger) *khttp.Server {
	srv := khttp.NewServer(
		khttp.Address(cfg.HTTPAddr),
		khttp.Timeout(cfg.HTTPTimeout),
		khttp.Filter(corsFilter, spaFilter),
	)
	srv.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	root := srv.Route("/")
	root.GET("/api/v1/meta", func(ctx khttp.Context) error {
		return ctx.JSON(http.StatusOK, map[string]any{
			"service":    "ozon-workbench",
			"mock":       cfg.OzonMock,
			"codex_mock": cfg.CodexMock,
			"phase":      2,
		})
	})
	api := srv.Route("/api/v1")
	shops.Register(api)
	categories.Register(api)
	tasks.Register(api)
	srv.HandlePrefix("/media/", http.HandlerFunc(tasks.ServeMedia))
	log.NewHelper(logger).Infof("http listening on %s ozon_mock=%v codex_mock=%v", cfg.HTTPAddr, cfg.OzonMock, cfg.CodexMock)
	return srv
}

func corsFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Shop-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
