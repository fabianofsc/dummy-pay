package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/payment"
)

// ClaimedWork is one row claimed from outbox_work: enough for the caller to
// dispatch on Kind and load whatever SubjectID references (spec §5).
type ClaimedWork struct {
	ID        uuid.UUID
	Kind      payment.OutboxKind
	SubjectID uuid.UUID
}

// ClaimDueWork atomically claims up to batch PENDING rows due at or before
// now, in a single UPDATE ... RETURNING that marks them DONE as it claims
// them. FOR UPDATE SKIP LOCKED means two workers calling this concurrently
// never claim the same row — the second worker's SELECT simply skips
// whatever the first has already locked (spec §5).
//
// Claiming a row as DONE, rather than to some intermediate "in progress"
// state, is deliberate: outbox_work answers only "should this be attempted",
// once; the actual outcome — a payment's status, a delivery's SENT/FAILED —
// lives in its own table, per spec §3's division of responsibility. A
// SETTLE_PAYMENT that turns out to be a no-op (already settled) and a
// DELIVER_WEBHOOK that fails are both still "attempted", and neither
// automatically retries (spec §5 — retry is the explicit endpoint, Phase 10).
func ClaimDueWork(ctx context.Context, pool *pgxpool.Pool, now time.Time, batch int) ([]ClaimedWork, error) {
	rows, err := querier(ctx, pool).Query(ctx,
		`UPDATE outbox_work SET state = 'DONE', claimed_at = $1
		 WHERE id IN (
		   SELECT id FROM outbox_work
		   WHERE state = 'PENDING' AND due_at <= $1
		   ORDER BY due_at
		   LIMIT $2
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, kind, subject_id`,
		now, batch,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claimed []ClaimedWork
	for rows.Next() {
		var (
			id        uuid.UUID
			kind      string
			subjectID uuid.UUID
		)
		if err := rows.Scan(&id, &kind, &subjectID); err != nil {
			return nil, err
		}
		claimed = append(claimed, ClaimedWork{
			ID:        id,
			Kind:      payment.OutboxKind(kind),
			SubjectID: subjectID,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return claimed, nil
}

// OutboxClaimer is the Postgres adapter for payment.OutboxClaimer, wrapping
// ClaimDueWork — the free function Step 9.1's tests exercise directly — so
// the worker can depend on the domain port rather than this package.
type OutboxClaimer struct {
	pool *pgxpool.Pool
}

// NewOutboxClaimer constructs an OutboxClaimer against pool.
func NewOutboxClaimer(pool *pgxpool.Pool) *OutboxClaimer {
	return &OutboxClaimer{pool: pool}
}

// ClaimDue implements payment.OutboxClaimer.
func (c *OutboxClaimer) ClaimDue(ctx context.Context, now time.Time, batch int) ([]payment.ClaimedWork, error) {
	rows, err := ClaimDueWork(ctx, c.pool, now, batch)
	if err != nil {
		return nil, err
	}
	claimed := make([]payment.ClaimedWork, len(rows))
	for i, r := range rows {
		claimed[i] = payment.ClaimedWork{ID: r.ID, Kind: r.Kind, SubjectID: r.SubjectID}
	}
	return claimed, nil
}
