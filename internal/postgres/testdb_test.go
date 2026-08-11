package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"dummypay/migrations"
)

// dsnEnvVar is the same variable internal/config reads (spec §9).
const dsnEnvVar = "DUMMYPAY_DATABASE_URL"

// NewTestDB provisions a uniquely named PostgreSQL schema, applies every
// embedded migration (migrations.FS) against it via goose, and returns a
// pool whose connections default to that schema through search_path.
//
// This is "one schema per test" per ADR-0013 — never a transaction rolled
// back per test. Later concurrency tests (idempotency claims, worker
// claiming) need genuinely separate, genuinely committed transactions
// racing each other, which a per-test rollback would make impossible to
// observe.
//
// If the database is unreachable: fails the test loudly when CI is set
// (a skip must never pass silently in CI, ADR-0013 Compliance), otherwise
// skips with instructions for local development.
func NewTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn, ok := os.LookupEnv(dsnEnvVar)
	if !ok || dsn == "" {
		unavailable(t, fmt.Sprintf("%s is not set", dsnEnvVar))
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		unavailable(t, fmt.Sprintf("cannot connect to %s: %v", dsnEnvVar, err))
		return nil
	}
	defer probe.Close(ctx)
	if err := probe.Ping(ctx); err != nil {
		unavailable(t, fmt.Sprintf("cannot ping database: %v", err))
		return nil
	}

	schema := "test_" + randomHex(t)
	if _, err := probe.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	// Every connection made against this DSN is scoped to the fresh schema
	// via the search_path runtime parameter, rather than qualifying every
	// migration and query with a `<schema>.` prefix. pg_catalog (uuid,
	// text, etc.) is always implicitly searched first regardless of this
	// setting, so only application objects are affected.
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", dsnEnvVar, err)
	}
	connConfig.RuntimeParams["search_path"] = schema

	sqlDB := stdlib.OpenDB(*connConfig)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		sqlDB.Close()
		t.Fatalf("create goose provider: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		sqlDB.Close()
		t.Fatalf("apply migrations to schema %s: %v", schema, err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close migration connection: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatalf("open pool for schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		if _, err := pool.Exec(dropCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
		pool.Close()
	})

	return pool
}

// unavailable reports the database as unreachable: a hard failure under CI
// (GitHub Actions sets CI=true automatically — ADR-0013 Compliance), a skip
// for local development.
func unavailable(t *testing.T, reason string) {
	t.Helper()
	if _, ci := os.LookupEnv("CI"); ci {
		t.Fatalf("integration test requires a reachable database but %s (start one with `make db-up`)", reason)
	}
	t.Skipf("skipping integration test: %s (start one with `make db-up`)", reason)
}

// randomHex returns 16 hex characters from crypto/rand, so schema names
// from concurrent test runs cannot collide the way an unseeded math/rand
// sequence could.
func randomHex(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate random schema suffix: %v", err)
	}
	return hex.EncodeToString(buf)
}
