package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// V2Migrations 是 V2 进程会执行的脚本。V1 的 001–004 只保留作历史，不会在这里运行，也不会删除旧表。
func V2Migrations() []string {
	return []string{"0001_v2_init.sql", "0002_app_support.sql"}
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return err
	}
	names := append([]string{}, V2Migrations()...)
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("读取迁移 %s: %w", name, err)
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return err
		}
		result := conn.Conn().PgConn().Exec(ctx, string(body))
		execErr := result.Close()
		conn.Release()
		if execErr != nil {
			return fmt.Errorf("执行迁移 %s: %w", name, execErr)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, name); err != nil {
			return err
		}
	}
	return nil
}
