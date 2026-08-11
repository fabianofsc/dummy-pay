package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/payment"
)

// OutboxWriter is the Postgres adapter for payment.OutboxWriter: it enqueues
// scheduled work as rows with a due time rather than as timers, so a restart
// loses nothing and tests need no sleep (spec §3 "outbox_work", §8,
// ADR-0008, ADR-0012).
type OutboxWriter struct {
	pool  *pgxpool.Pool
	ids   payment.IDGenerator
	clock payment.Clock
}

// NewOutboxWriter constructs an OutboxWriter against pool, minting work-row
// identifiers with ids and stamping created_at from clock — never from
// time.Now(), which no package outside internal/clock and cmd/ may call
// (ADR-0012).
func NewOutboxWriter(pool *pgxpool.Pool, ids payment.IDGenerator, clock payment.Clock) *OutboxWriter {
	return &OutboxWriter{pool: pool, ids: ids, clock: clock}
}

// Enqueue writes one PENDING work row due at dueAt.
//
// Like every other repository here it goes through querier(ctx, w.pool), so
// when the create-payment flow calls it inside TxManager.Within the row is
// written in that same transaction. That is what spec §4.1 means by
// "everything above is one transaction": a payment cannot be committed
// without its settlement enqueued, and an enqueue cannot survive a payment
// that was rolled back.
func (w *OutboxWriter) Enqueue(ctx context.Context, kind payment.OutboxKind, subjectID uuid.UUID, dueAt time.Time) error {
	_, err := querier(ctx, w.pool).Exec(ctx,
		`INSERT INTO outbox_work (id, kind, subject_id, due_at, state, created_at)
		 VALUES ($1, $2, $3, $4, 'PENDING', $5)`,
		w.ids.NewID(), string(kind), subjectID, dueAt, w.clock.Now(),
	)
	return err
}
