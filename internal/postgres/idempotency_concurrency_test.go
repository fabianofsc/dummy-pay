package postgres

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

// TestIdempotencyStore_Claim_ConcurrentSameKey_ExactlyOneWinner is
// docs/plan-v1.md Step 4.3. It drives no new production code — it proves
// that the Claim built in Step 4.2 (internal/postgres/idempotency.go, an
// INSERT ... ON CONFLICT (account_id, idempotency_key) DO NOTHING RETURNING)
// really is settled by the database rather than by incidental goroutine
// scheduling (ADR-0007: "the race is settled by the database, not the
// application").
//
// N=20 goroutines call store.Claim for the same (accountID, key)
// simultaneously, released from a shared start barrier so the calls fire as
// close together as the runtime allows. Each goroutine carries a
// distinguishable fingerprint and claimedAt (derived from its index), so
// that after the race Load's stored values can be checked against exactly
// the winning goroutine's inputs — proving the database decided, not a
// Go-side assumption about which goroutine "should" have won.
//
// Per the plan's own text for this step: "If it fails, the design is wrong,
// not the test."
func TestIdempotencyStore_Claim_ConcurrentSameKey_ExactlyOneWinner(t *testing.T) {
	const n = 20

	pool := NewTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_claim_concurrent", base)
	require.NoError(t, err)

	store := NewIdempotencyStore(pool)
	const key = "concurrent-key"

	// Distinguishable per goroutine: a different reference_id fingerprints
	// differently (payment.Fingerprint), and claimedAt is offset by index.
	fingerprints := make([][32]byte, n)
	claimedAts := make([]time.Time, n)
	for i := 0; i < n; i++ {
		fingerprints[i] = testFingerprint(t, fmt.Sprintf("checkout:concurrent-%d", i))
		claimedAts[i] = base.Add(time.Duration(i) * time.Millisecond)
	}

	var (
		ready sync.WaitGroup // each goroutine signals readiness before blocking on start
		done  sync.WaitGroup
		start = make(chan struct{})

		winCount     atomic.Int64
		winningIndex atomic.Int64 // -1 until a winning Claim records its index

		errMu sync.Mutex
		errs  []error
	)
	winningIndex.Store(-1)

	ready.Add(n)
	done.Add(n)
	for i := range n {
		go func() {
			defer done.Done()
			ready.Done()
			<-start // released all at once, below, once every goroutine is waiting

			ok, err := store.Claim(ctx, accountID, key, fingerprints[i], claimedAts[i])
			if err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
				return
			}
			if ok {
				winCount.Add(1)
				winningIndex.Store(int64(i))
			}
		}()
	}

	ready.Wait() // every goroutine is registered and parked on start
	close(start) // fire them as close to simultaneously as the runtime allows
	done.Wait()

	for _, e := range errs {
		t.Errorf("Claim returned an error instead of a clean ok=false loss: %v", e)
	}

	require.EqualValues(t, 1, winCount.Load(), "exactly one goroutine must win the claim")

	idx := winningIndex.Load()
	require.GreaterOrEqual(t, idx, int64(0), "a winning index must have been recorded")

	// The stored record must carry exactly the winning goroutine's
	// fingerprint and claimedAt — not any loser's, and not a mix. Because
	// every goroutine's values are pairwise distinguishable, this is proof
	// the database (not this test's bookkeeping) decided the winner.
	got, err := store.Load(ctx, accountID, key)
	require.NoError(t, err)

	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprints[idx],
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          claimedAts[idx],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after the race did not match the recorded winner's values (-want +got):\n%s", diff)
	}
}
