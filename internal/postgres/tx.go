package postgres

import (
	"context"
	"fmt"

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

// querier returns the transaction TxManager bound into ctx, if any,
// otherwise pool. Repositories call it instead of touching pool directly,
// which is the whole mechanism by which they participate in a transaction
// without any of them taking a transaction-shaped argument (spec §8).
func querier(ctx context.Context, pool *pgxpool.Pool) pgxQuerier {
	if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// TxManager is the Postgres adapter for payment.TxManager. It runs fn inside
// one real transaction, binding it into ctx under txContextKey so that every
// repository call fn makes — each going through querier(ctx, pool) — lands in
// that transaction. This is what spec §4.1 means by "everything above is one
// transaction": a payment cannot be committed without its outbox work
// enqueued and its idempotency record completed (ADR-0008).
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager constructs a TxManager against pool.
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// Within begins a transaction, runs fn with it bound into the context, and
// commits if fn succeeds or rolls back if it does not.
//
// fn's error is what the caller gets back, unwrapped, so the domain's
// sentinel errors survive the round trip. A rollback that itself fails is
// folded into that error rather than replacing it: the original failure is
// what a caller branches on, while a broken rollback is context an operator
// needs and nothing else reports.
//
// There is no deferred rollback here. pgx tolerates a rollback after commit —
// it returns pgx.ErrTxClosed — but relying on that would mean the commit path
// routinely produces an error to ignore, and an error worth ignoring is one
// step from an error wrongly ignored. Each path ends the transaction exactly
// once instead.
func (m *TxManager) Within(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := fn(context.WithValue(ctx, txContextKey, tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
