package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txContextKeyType struct{}

var txContextKey = txContextKeyType{}

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so every
// repository method works identically inside and outside a transaction.
type pgxQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// querier returns the transaction a caller bound into ctx, if any,
// otherwise pool. A later task adds the TxManager that populates
// txContextKey; until then this always falls through to pool.
func querier(ctx context.Context, pool *pgxpool.Pool) pgxQuerier {
	if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return tx
	}
	return pool
}
