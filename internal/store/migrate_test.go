package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFilesExist(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	for _, name := range V2Migrations() {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigratePostgres(t *testing.T) {
	url := os.Getenv("OZON_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("OZON_TEST_DATABASE_URL 未设置")
	}
	ctx := context.Background()
	db, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db.Pool(), filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db.Pool(), filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	var shops int
	if err := db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name='ozon_shops' AND table_schema=current_schema()`).Scan(&shops); err != nil {
		t.Fatal(err)
	}
	if shops != 9 {
		t.Fatalf("ozon_shops columns = %d", shops)
	}
}
