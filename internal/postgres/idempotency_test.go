package postgres

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

// testFingerprint returns the fingerprint of a request whose reference_id is
// referenceID and whose other fields are fixed, so two calls with different
// referenceIDs are guaranteed to differ.
func testFingerprint(t *testing.T, referenceID string) [32]byte {
	t.Helper()
	amount, err := payment.NewAmount(10990)
	require.NoError(t, err)
	return payment.Fingerprint(referenceID, amount, payment.CurrencyBRL, payment.TokenCardApproved)
}

func TestIdempotencyStore_Claim_FreshKey_Succeeds(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_claim_fresh", now)
	require.NoError(t, err)

	store := NewIdempotencyStore(pool)
	fp := testFingerprint(t, "checkout:123")

	ok, err := store.Claim(ctx, accountID, "key-1", fp, now)
	require.NoError(t, err)
	require.True(t, ok, "a fresh key must be claimable")

	got, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	// PaymentID, ResponseStatus, ResponseBody and CompletedAt are left at
	// their zero values on purpose: those columns are NULL until Complete
	// runs, and this asserts a NULL scans back as the zero value rather than
	// as garbage or an error.
	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     "key-1",
		RequestFingerprint: fp,
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          now,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after Claim (-want +got):\n%s", diff)
	}
}

// TestIdempotencyStore_Claim_HeldKey_ReturnsNotOkAndKeepsFirstClaim proves the
// second claimant of a key gets ok=false with a nil error — "already held" is
// the normal outcome of a race, not a failure — and that nothing about the
// first claim was overwritten: the stored fingerprint and claimed_at are still
// the first claimant's, even though the second claim carried different values.
func TestIdempotencyStore_Claim_HeldKey_ReturnsNotOkAndKeepsFirstClaim(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	first := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	second := first.Add(time.Second)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_claim_held", first)
	require.NoError(t, err)

	store := NewIdempotencyStore(pool)
	firstFP := testFingerprint(t, "checkout:123")
	secondFP := testFingerprint(t, "checkout:999")
	require.NotEqual(t, firstFP, secondFP, "the two fixtures must fingerprint differently")

	ok, err := store.Claim(ctx, accountID, "key-1", firstFP, first)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Claim(ctx, accountID, "key-1", secondFP, second)
	require.NoError(t, err, "claiming a held key is not an error")
	require.False(t, ok, "the second claimant must not win the key")

	got, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     "key-1",
		RequestFingerprint: firstFP,
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          first,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after a losing Claim (-want +got):\n%s", diff)
	}
}

// TestIdempotencyStore_Reclaim_WithinLease_ReturnsNotOk pins the *boundary* of
// the lease rather than an obviously-live claim: cutoff is set to exactly the
// row's claimed_at, so the SQL's `claimed_at < $cutoff` must be false by a
// margin of zero. A `<=` written where `<` belongs would reclaim here and this
// test would catch it; a cutoff comfortably older than claimed_at would not.
//
// "Still inside its lease" is a lost race, not a fault, so this is
// ok=false with a nil error (spec §4.1: an IN_FLIGHT row within its lease
// yields 409, decided by the caller, not by an error from the store).
func TestIdempotencyStore_Reclaim_WithinLease_ReturnsNotOk(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_reclaim_within", t0)
	require.NoError(t, err)

	store := NewIdempotencyStore(pool)
	fp := testFingerprint(t, "checkout:123")

	ok, err := store.Claim(ctx, accountID, "key-1", fp, t0)
	require.NoError(t, err)
	require.True(t, ok)

	// cutoff == claimed_at exactly: the lease boundary is *now*, so a row
	// claimed at that instant is not yet stale.
	ok, err = store.Reclaim(ctx, accountID, "key-1", t0, t0.Add(time.Hour))
	require.NoError(t, err, "a live lease is a lost race, not an error")
	require.False(t, ok, "a row claimed at exactly the cutoff must not be reclaimable")

	got, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	// Every field is asserted, not just ClaimedAt: a botched UPDATE that
	// matched no row is indistinguishable from one that matched and wrote
	// nothing unless the whole record is checked.
	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     "key-1",
		RequestFingerprint: fp,
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          t0,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after a refused Reclaim (-want +got):\n%s", diff)
	}
}

// TestIdempotencyStore_Reclaim_ExpiredLease_TakesOverAndResetsClaimedAt is the
// other side of the boundary: the same shape of row, with cutoff moved one
// millisecond past claimed_at, is reclaimable. claimed_at must become the new
// owner's `now`, because that value is what starts the *reclaimer's* lease —
// leaving the original timestamp would make the key instantly re-reclaimable
// by everyone else.
func TestIdempotencyStore_Reclaim_ExpiredLease_TakesOverAndResetsClaimedAt(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(45 * time.Second)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_reclaim_expired", t0)
	require.NoError(t, err)

	store := NewIdempotencyStore(pool)
	fp := testFingerprint(t, "checkout:123")

	ok, err := store.Claim(ctx, accountID, "key-1", fp, t0)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Reclaim(ctx, accountID, "key-1", t0.Add(time.Millisecond), t1)
	require.NoError(t, err)
	require.True(t, ok, "a row claimed before the cutoff must be reclaimable")

	got, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	// State stays IN_FLIGHT and the fingerprint is untouched: reclaiming
	// transfers ownership of the same request, it does not start a different
	// one. Only claimed_at moves.
	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     "key-1",
		RequestFingerprint: fp,
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          t1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after a successful Reclaim (-want +got):\n%s", diff)
	}
}

// TestIdempotencyStore_Reclaim_CompletedRecord_ReturnsNotOk guards the state
// half of the WHERE clause. A COMPLETED row whose claimed_at is long past the
// cutoff satisfies the timestamp condition entirely; only `state = 'IN_FLIGHT'`
// stops it being reclaimed, which would strand a recorded replay response
// behind a fresh IN_FLIGHT lease.
func TestIdempotencyStore_Reclaim_CompletedRecord_ReturnsNotOk(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	completedAt := t0.Add(40 * time.Millisecond)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_reclaim_completed", t0)
	require.NoError(t, err)

	payments := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardApproved, t0)
	require.NoError(t, payments.Insert(ctx, p))

	store := NewIdempotencyStore(pool)
	fp := testFingerprint(t, "checkout:123")

	ok, err := store.Claim(ctx, accountID, "key-1", fp, t0)
	require.NoError(t, err)
	require.True(t, ok)

	body := []byte(`{"status":"APPROVED"}`)
	require.NoError(t, store.Complete(ctx, accountID, "key-1", p.ID, 201, body, completedAt))

	// The timestamp condition is satisfied by a wide margin; state is the only
	// thing refusing this.
	ok, err = store.Reclaim(ctx, accountID, "key-1", t0.Add(time.Hour), t0.Add(2*time.Hour))
	require.NoError(t, err)
	require.False(t, ok, "a COMPLETED record must never be reclaimable")

	got, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     "key-1",
		RequestFingerprint: fp,
		State:              payment.IdempotencyCompleted,
		PaymentID:          p.ID,
		ResponseStatus:     201,
		ResponseBody:       body,
		ClaimedAt:          t0,
		CompletedAt:        completedAt,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after Reclaim refused a COMPLETED record (-want +got):\n%s", diff)
	}
}

func TestIdempotencyStore_Complete_StoresPaymentResponseAndTimestamp(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	claimedAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	completedAt := claimedAt.Add(40 * time.Millisecond)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_complete", claimedAt)
	require.NoError(t, err)

	payments := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardApproved, claimedAt)
	require.NoError(t, payments.Insert(ctx, p))

	store := NewIdempotencyStore(pool)
	fp := testFingerprint(t, "checkout:123")

	ok, err := store.Claim(ctx, accountID, "key-1", fp, claimedAt)
	require.NoError(t, err)
	require.True(t, ok)

	body := []byte(`{"payment_id":"pay_x",  "status":"APPROVED"}`)
	require.NoError(t, store.Complete(ctx, accountID, "key-1", p.ID, 201, body, completedAt))

	got, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	want := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     "key-1",
		RequestFingerprint: fp,
		State:              payment.IdempotencyCompleted,
		PaymentID:          p.ID,
		ResponseStatus:     201,
		ResponseBody:       body,
		ClaimedAt:          claimedAt,
		CompletedAt:        completedAt,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load after Complete (-want +got):\n%s", diff)
	}
}

// TestIdempotencyStore_Load_Replay_ReturnsByteIdenticalBody proves the replay
// path returns the stored response bytes verbatim rather than a
// re-serialisation of them: the fixture body carries irregular whitespace and
// non-alphabetical key order, both of which a jsonb round-trip would destroy
// (spec §3, "response_body is stored as raw bytes, not jsonb").
func TestIdempotencyStore_Load_Replay_ReturnsByteIdenticalBody(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	claimedAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	completedAt := claimedAt.Add(40 * time.Millisecond)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_replay", claimedAt)
	require.NoError(t, err)

	payments := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardApproved, claimedAt)
	require.NoError(t, payments.Insert(ctx, p))

	store := NewIdempotencyStore(pool)
	fp := testFingerprint(t, "checkout:123")

	ok, err := store.Claim(ctx, accountID, "key-1", fp, claimedAt)
	require.NoError(t, err)
	require.True(t, ok)

	body := []byte("{\n  \"status\" : \"APPROVED\",\n  \"amount\":10990\n}")
	require.NoError(t, store.Complete(ctx, accountID, "key-1", p.ID, 201, body, completedAt))

	firstLoad, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)
	secondLoad, err := store.Load(ctx, accountID, "key-1")
	require.NoError(t, err)

	require.True(t, bytes.Equal(body, firstLoad.ResponseBody),
		"first replay returned different bytes than were stored: want %q, got %q", body, firstLoad.ResponseBody)
	require.True(t, bytes.Equal(firstLoad.ResponseBody, secondLoad.ResponseBody),
		"two replays returned different bytes: %q vs %q", firstLoad.ResponseBody, secondLoad.ResponseBody)
	if diff := cmp.Diff(firstLoad, secondLoad); diff != "" {
		t.Errorf("two replays returned different records (-first +second):\n%s", diff)
	}
}

// TestIdempotencyStore_Complete_UnknownKey_ReturnsErrIdempotencyRecordNotFound
// guards the worst failure mode this adapter has: a Complete that matches no
// row must not report success. If it did, the caller would believe a response
// had been recorded for replay when nothing was written, and every later retry
// of that key would see IN_FLIGHT forever.
func TestIdempotencyStore_Complete_UnknownKey_ReturnsErrIdempotencyRecordNotFound(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	store := NewIdempotencyStore(pool)

	err := store.Complete(ctx, uuid.New(), "never-claimed", uuid.New(), 201, []byte(`{}`), now)
	require.ErrorIs(t, err, payment.ErrIdempotencyRecordNotFound)
}

func TestIdempotencyStore_Load_UnknownKey_ReturnsErrIdempotencyRecordNotFound(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()

	store := NewIdempotencyStore(pool)

	_, err := store.Load(ctx, uuid.New(), "never-claimed")
	require.ErrorIs(t, err, payment.ErrIdempotencyRecordNotFound)
}
