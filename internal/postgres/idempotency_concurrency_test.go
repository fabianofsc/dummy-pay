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

// TestIdempotencyStore_Reclaim_ConcurrentExpiredLease_ExactlyOneWinner is the
// second half of docs/plan-v1.md Step 4.4: "two concurrent reclaims resolve
// with exactly one winner". It proves that reclaiming an abandoned lease is
// settled by the database on the same terms as the original Claim
// (ADR-0007) — a conditional UPDATE, not a read-then-write that two processes
// could both pass.
//
// N=20 goroutines call store.Reclaim for one already-expired IN_FLIGHT row,
// released from a shared start barrier.
//
// Unlike the Claim race above, every goroutine here passes the *same* cutoff
// and the *same* now, and that is deliberate — it is what makes "exactly one
// winner" a property of the SQL rather than of scheduling. Concurrent UPDATEs
// against one row serialise: under READ COMMITTED the second waits for the
// first to commit and then re-evaluates its WHERE clause against the row as
// updated. The winner has set claimed_at = now, and now is later than cutoff
// (cutoff is a lease boundary in the past), so `claimed_at < cutoff` is false
// for every later goroutine and each cleanly reports ok=false.
//
// Handing each goroutine a distinguishable now — the pattern the Claim test
// above uses for its inputs — would break exactly that: whether a second
// reclaim succeeded would depend on how its now compared with the winner's,
// making the outcome order-dependent and the assertion meaningless.
func TestIdempotencyStore_Reclaim_ConcurrentExpiredLease_ExactlyOneWinner(t *testing.T) {
	const n = 20

	pool := NewTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_reclaim_concurrent", t0)
	require.NoError(t, err)

	store := NewIdempotencyStore(pool)
	const key = "abandoned-key"
	fp := testFingerprint(t, "checkout:abandoned")

	// The row every goroutine will race to take over: claimed at t0 by an
	// owner that never completed — the crashed-mid-flight case ADR-0007
	// describes.
	ok, err := store.Claim(ctx, accountID, key, fp, t0)
	require.NoError(t, err)
	require.True(t, ok)

	// Shared by all n goroutines. cutoff is past t0, so the row starts out
	// reclaimable; now is past cutoff, so the winner's write makes the row
	// un-reclaimable for everyone else.
	cutoff := t0.Add(30 * time.Second)
	now := t0.Add(45 * time.Second)
	require.True(t, now.After(cutoff), "now must be past cutoff or no winner can exclude the others")

	var (
		ready sync.WaitGroup // each goroutine signals readiness before blocking on start
		done  sync.WaitGroup
		start = make(chan struct{})

		winCount atomic.Int64

		errMu sync.Mutex
		errs  []error
	)

	ready.Add(n)
	done.Add(n)
	for range n {
		go func() {
			defer done.Done()
			ready.Done()
			<-start // released all at once, below, once every goroutine is waiting

			ok, err := store.Reclaim(ctx, accountID, key, cutoff, now)
			if err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
				return
			}
			if ok {
				winCount.Add(1)
			}
		}()
	}

	ready.Wait() // every goroutine is registered and parked on start
	close(start) // fire them as close to simultaneously as the runtime allows
	done.Wait()

	// A losing reclaim must be ok=false with a nil error, exactly as a losing
	// Claim is. Any error at all is a failure of the contract, not a loss.
	for _, e := range errs {
		t.Errorf("Reclaim returned an error instead of a clean ok=false loss: %v", e)
	}

	require.EqualValues(t, 1, winCount.Load(), "exactly one goroutine must win the reclaim")

	got, err := store.Load(ctx, accountID, key)
	require.NoError(t, err)

	// claimed_at is the shared now — written once. If two goroutines had both
	// passed their UPDATE's WHERE clause, winCount would already have caught
	// it; this confirms the surviving row is the reclaimer's and that nothing
	// else about the record moved.
	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     key,
		RequestFingerprint: fp,
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          now,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after the reclaim race (-want +got):\n%s", diff)
	}
}
