package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

func init() {
	goose.SetBaseFS(migrationsFS)
	_ = goose.SetDialect("postgres")
}

// collectMigrations parses the embedded migrations without touching a database.
func collectMigrations() (goose.Migrations, error) {
	return goose.CollectMigrations(migrationsDir, 0, goose.MaxVersion)
}

// MigrateUp applies all pending migrations against the database at dsn.
func MigrateUp(ctx context.Context, dsn string) error {
	return withDB(dsn, func(db *sql.DB) error {
		return goose.UpContext(ctx, db, migrationsDir)
	})
}

// MigrateDownTo rolls migrations back to the target version (0 = empty schema).
func MigrateDownTo(ctx context.Context, dsn string, version int64) error {
	return withDB(dsn, func(db *sql.DB) error {
		return goose.DownToContext(ctx, db, migrationsDir, version)
	})
}

// Version reports the currently-applied migration version (0 if none).
func Version(ctx context.Context, dsn string) (int64, error) {
	var v int64
	err := withDB(dsn, func(db *sql.DB) error {
		var verr error
		v, verr = goose.GetDBVersionContext(ctx, db)
		return verr
	})
	return v, err
}

func withDB(dsn string, fn func(*sql.DB) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("store: open db: %w", err)
	}
	defer db.Close()
	return fn(db)
}
