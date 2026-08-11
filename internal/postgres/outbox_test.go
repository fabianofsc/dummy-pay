package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	"dummypay/internal/payment"
)

func TestOutboxWriter_Enqueue_PersistsPendingWork(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(3 * time.Second)

	ids := &recordingIDGenerator{}
	writer := NewOutboxWriter(pool, ids, clock.NewFake(now))

	subjectID := uuid.New()
	require.NoError(t, writer.Enqueue(ctx, payment.OutboxSettlePayment, subjectID, dueAt))

	require.Len(t, ids.issued, 1)

	var (
		id        uuid.UUID
		kind      string
		subject   uuid.UUID
		gotDueAt  time.Time
		state     string
		createdAt time.Time
	)
	err := pool.QueryRow(ctx,
		`SELECT id, kind, subject_id, due_at, state, created_at FROM outbox_work`,
	).Scan(&id, &kind, &subject, &gotDueAt, &state, &createdAt)
	require.NoError(t, err)

	require.Equal(t, ids.issued[0], id)
	require.Equal(t, string(payment.OutboxSettlePayment), kind)
	require.Equal(t, subjectID, subject)
	require.True(t, dueAt.Equal(gotDueAt), "want due_at %s, got %s", dueAt, gotDueAt)
	require.Equal(t, "PENDING", state)
	require.True(t, now.Equal(createdAt), "created_at must come from the injected clock: want %s, got %s", now, createdAt)
}

// TestOutboxWriter_Enqueue_ParticipatesInTheTransaction proves Enqueue routes
// through querier(ctx, pool) rather than the pool directly: enqueued inside a
// transaction that then fails, the work row must be gone. Were it writing on
// its own connection, the row would survive the rollback and the outbox would
// schedule work for a payment that was never committed (spec §4.1, ADR-0008).
func TestOutboxWriter_Enqueue_ParticipatesInTheTransaction(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	writer := NewOutboxWriter(pool, &recordingIDGenerator{}, clock.NewFake(now))

	err := NewTxManager(pool).Within(ctx, func(ctx context.Context) error {
		if err := writer.Enqueue(ctx, payment.OutboxDeliverWebhook, uuid.New(), now); err != nil {
			return err
		}
		return errOutboxUnavailable
	})
	require.ErrorIs(t, err, errOutboxUnavailable)

	var total int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM outbox_work`).Scan(&total))
	require.Equal(t, 0, total)
}
