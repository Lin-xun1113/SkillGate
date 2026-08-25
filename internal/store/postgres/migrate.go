package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Migrate(ctx context.Context, db *sql.DB) error {
	provider, err := migrationProvider(db)
	if err != nil {
		return fmt.Errorf("创建 migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("执行 migration: %w", err)
	}
	return nil
}

func MigrationVersions(ctx context.Context, db *sql.DB) (current, target int64, err error) {
	provider, err := migrationProvider(db)
	if err != nil {
		return 0, 0, fmt.Errorf("创建 migration provider: %w", err)
	}
	current, target, err = provider.GetVersions(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("读取 migration 版本: %w", err)
	}
	return current, target, nil
}

func migrationProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("读取嵌入式 migration: %w", err)
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations)
}
