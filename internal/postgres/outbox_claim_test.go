package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	"dummypay/internal/payment"
)

// TestClaimDueWork_OnlyDueWorkIsClaimed verifies that a row due in the
// future is left PENDING while a row due at or before now is claimed
// (spec §5).
func TestClaimDueWork_OnlyDueWorkIsClaimed(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	writer := NewOutboxWriter(pool, &recordingIDGenerator{}, clock.NewFake(now))

	dueSubject := uuid.New()
	futureSubject := uuid.New()
	require.NoError(t, writer.Enqueue(ctx, payment.OutboxSettlePayment, dueSubject, now))
	require.NoError(t, writer.Enqueue(ctx, payment.OutboxSettlePayment, futureSubject, now.Add(1*time.Hour)))

	claimed, err := ClaimDueWork(ctx, pool, now, 10)
	require.NoError(t, err)

	require.Len(t, claimed, 1)
	require.Equal(t, dueSubject, claimed[0].SubjectID)
	require.Equal(t, payment.OutboxSettlePayment, claimed[0].Kind)

	// The future row must still be PENDING.
	var state string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT state FROM outbox_work WHERE subject_id = $1`, futureSubject,
	).Scan(&state))
	require.Equal(t, "PENDING", state)

	// The claimed row must now be DONE.
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT state FROM outbox_work WHERE subject_id = $1`, dueSubject,
	).Scan(&state))
	require.Equal(t, "DONE", state)
}

// TestClaimDueWork_RespectsLimit verifies that claiming honours the batch
// size, leaving the rest PENDING.
func TestClaimDueWork_RespectsLimit(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	writer := NewOutboxWriter(pool, &recordingIDGenerator{}, clock.NewFake(now))

	for i := 0; i < 5; i++ {
		require.NoError(t, writer.Enqueue(ctx, payment.OutboxSettlePayment, uuid.New(), now))
	}

	claimed, err := ClaimDueWork(ctx, pool, now, 3)
	require.NoError(t, err)
	require.Len(t, claimed, 3)

	var pendingCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_work WHERE state = 'PENDING'`,
	).Scan(&pendingCount))
	require.Equal(t, 2, pendingCount)
}

// TestClaimDueWork_ConcurrentWorkers_NeverClaimTheSameRow proves FOR UPDATE
// SKIP LOCKED against a real database: two workers claiming concurrently
// from the same due set never see an overlapping row (spec §5).
func TestClaimDueWork_ConcurrentWorkers_NeverClaimTheSameRow(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	writer := NewOutboxWriter(pool, &recordingIDGenerator{}, clock.NewFake(now))

	const totalWork = 20
	for i := 0; i < totalWork; i++ {
		require.NoError(t, writer.Enqueue(ctx, payment.OutboxSettlePayment, uuid.New(), now))
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed []ClaimedWork
	)

	const workers = 4
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ClaimDueWork(ctx, pool, now, totalWork)
			require.NoError(t, err)
			mu.Lock()
			claimed = append(claimed, got...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	require.Len(t, claimed, totalWork, "every row must be claimed exactly once across all workers")

	seen := make(map[uuid.UUID]bool, totalWork)
	for _, c := range claimed {
		require.False(t, seen[c.ID], "work item %s claimed more than once", c.ID)
		seen[c.ID] = true
	}
}
