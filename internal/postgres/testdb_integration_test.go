package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"dummypay/internal/postgres"
	"dummypay/migrations"
)

// wantTables is the full table list a fresh schema should have after every
// migration in migrations/ has been applied, plus goose's own version
// tracking table. Sorted, to match the ORDER BY below.
var wantTables = []string{
	"accounts",
	"goose_db_version",
	"idempotency_keys",
	"outbox_work",
	"payments",
	"webhook_deliveries",
	"webhook_subscriptions",
}

func TestNewTestDB_CreatesSchemaMigratesAndCleansUp(t *testing.T) {
	var schema string

	t.Run("schema has all six tables plus goose's own", func(t *testing.T) {
		pool := postgres.NewTestDB(t)
		ctx := context.Background()

		require.NoError(t, pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema))
		require.NotEmpty(t, schema)

		require.Equal(t, wantTables, listTables(t, pool, schema))
	})

	if schema == "" {
		// The subtest skipped (no reachable database) rather than ran, so
		// there is nothing for cleanup to have dropped.
		t.Skip("skipping cleanup verification: subtest above did not run")
	}

	// The subtest above has returned, so its t.Cleanup has already run and
	// dropped the schema. Verify that with a fresh connection — proving
	// cleanup actually happened, not just that it was registered.
	requireSchemaAbsent(t, schema)
}

func TestNewTestDB_ParallelCallsDoNotCollide(t *testing.T) {
	var mu sync.Mutex
	var schemas []string

	for i := range 2 {
		t.Run(fmt.Sprintf("run-%d", i), func(t *testing.T) {
			t.Parallel()

			pool := postgres.NewTestDB(t)
			ctx := context.Background()

			var schema string
			require.NoError(t, pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema))
			require.Equal(t, wantTables, listTables(t, pool, schema))

			mu.Lock()
			schemas = append(schemas, schema)
			mu.Unlock()
		})
	}

	// Cleanup runs after this test and all its (including parallel)
	// subtests complete.
	t.Cleanup(func() {
		if len(schemas) == 0 {
			// Both subtests skipped (no reachable database); nothing to
			// compare.
			return
		}
		require.Len(t, schemas, 2)
		require.NotEqual(t, schemas[0], schemas[1])
	})
}

func TestMigrations_UpDownUpReachesSameSchema(t *testing.T) {
	dsn := requireDSN(t)
	pool := postgres.NewTestDB(t)
	ctx := context.Background()

	var schema string
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema))
	before := listTables(t, pool, schema)
	require.Equal(t, wantTables, before)

	connConfig, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	connConfig.RuntimeParams["search_path"] = schema

	sqlDB := stdlib.OpenDB(*connConfig)
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	require.NoError(t, err)

	_, err = provider.DownTo(ctx, 0)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	after := listTables(t, pool, schema)
	require.Equal(t, before, after)
}

// listTables returns the sorted table names in schema.
func listTables(t *testing.T, pool *pgxpool.Pool, schema string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1
		ORDER BY table_name`, schema)
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	sort.Strings(tables)
	return tables
}

// requireSchemaAbsent opens its own connection (independent of any pool a
// prior NewTestDB call may have already closed) and asserts schema no
// longer appears in information_schema.schemata.
func requireSchemaAbsent(t *testing.T, schema string) {
	t.Helper()
	if schema == "" {
		t.Fatalf("requireSchemaAbsent called with empty schema name")
	}

	dsn := requireDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var count int
	err = conn.QueryRow(ctx,
		"SELECT count(*) FROM information_schema.schemata WHERE schema_name = $1", schema,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "schema %s should have been dropped by cleanup", schema)
}

// requireDSN returns DUMMYPAY_DATABASE_URL or skips/fails the same way
// NewTestDB does — these auxiliary tests only make sense once NewTestDB
// itself has already proven the database reachable.
func requireDSN(t *testing.T) string {
	t.Helper()
	dsn, ok := os.LookupEnv("DUMMYPAY_DATABASE_URL")
	if !ok || dsn == "" {
		t.Skipf("DUMMYPAY_DATABASE_URL not set")
	}
	return dsn
}
