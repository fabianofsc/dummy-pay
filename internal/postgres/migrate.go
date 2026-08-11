package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"dummypay/migrations"
)

// Migrate applies every embedded migration (migrations.FS) against sqlDB, in
// order, idempotently — goose only applies migrations not yet recorded as
// run. It does not close sqlDB; the caller owns that connection's lifecycle
// (ADR-0011).
//
// Taking an already-open *sql.DB rather than a DSN string is what lets this
// same function serve two callers with different connection needs: the test
// harness (NewTestDB) opens one scoped to an isolated schema via
// search_path, and cmd/dummypay opens one against the database's default
// schema — both get the identical migration logic, so this path is
// genuinely exercised by every integration test rather than only by
// production, which no test runs.
func Migrate(ctx context.Context, sqlDB *sql.DB) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

// OpenForMigration opens a *sql.DB against dsn suitable for passing to
// Migrate — a thin wrapper so callers with a plain DSN (cmd/dummypay) don't
// need to reach into pgx/stdlib themselves.
func OpenForMigration(dsn string) (*sql.DB, error) {
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	return stdlib.OpenDB(*connConfig), nil
}
