package data

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-kratos/kratos/v2/log"
	_ "github.com/jackc/pgx/v5/stdlib"

	"ozon-workbench/internal/conf"
	"ozon-workbench/internal/data/ent"
)

// Data owns the Ent client and the underlying SQL pool.
type Data struct {
	Client *ent.Client
	db     *sql.DB
}

func NewData(cfg *conf.Runtime, logger log.Logger) (*Data, func(), error) {
	db, err := sql.Open("pgx", cfg.DatabaseDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("打开数据库: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	if err := client.Schema.Create(ctx); err != nil {
		client.Close()
		db.Close()
		return nil, nil, fmt.Errorf("迁移数据库: %w", err)
	}
	log.NewHelper(logger).Info("postgresql schema ready")
	cleanup := func() {
		client.Close()
		db.Close()
	}
	return &Data{Client: client, db: db}, cleanup, nil
}
