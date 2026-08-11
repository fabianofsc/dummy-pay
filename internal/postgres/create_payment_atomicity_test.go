package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	"dummypay/internal/payment"
)

// errOutboxUnavailable is the failure injected into the create-payment
// transaction. It stands in for anything that can go wrong after the payment
// row is written but before the flow finishes.
var errOutboxUnavailable = errors.New("outbox unavailable")

// failingOutboxWriter fails every Enqueue, and — before failing — reads the
// payments table through querier(ctx, pool), i.e. inside the caller's
// transaction.
//
// The read is what makes the rollback assertion mean anything. A test that
// only checked "no payment exists afterwards" could not tell a rolled-back
// INSERT from an INSERT that never ran at all: both leave an empty table. By
// recording that the row *was* visible inside the transaction at the moment
// the failure was injected, the test proves the transaction genuinely undid a
// write that had already happened.
//
// It writes nothing, so what is under test stays TxManager's rollback rather
// than the real OutboxWriter's SQL (which outbox_test.go covers separately).
type failingOutboxWriter struct {
	pool *pgxpool.Pool
	err  error

	// calls counts Enqueue invocations; the flow must fail on the first.
	calls int
	// subjectID is the id Enqueue was called with — for SETTLE_PAYMENT that
	// is the payment's id, so the test never has to guess which of the
	// generated identifiers the payment got.
	subjectID uuid.UUID
	// paymentsVisibleInTx is how many rows the payments table held, as seen
	// from inside the transaction, when Enqueue ran.
	paymentsVisibleInTx int
	// readErr records a failure of that read, so a broken probe cannot be
	// mistaken for "the payment was not there".
	readErr error
}

func (w *failingOutboxWriter) Enqueue(ctx context.Context, _ payment.OutboxKind, subjectID uuid.UUID, _ time.Time) error {
	w.calls++
	w.subjectID = subjectID
	w.readErr = querier(ctx, w.pool).QueryRow(ctx,
		`SELECT count(*) FROM payments WHERE id = $1`, subjectID,
	).Scan(&w.paymentsVisibleInTx)
	return w.err
}

// noSubscriptionRepository reports that the account has no active
// subscription, so no delivery is created and the settlement enqueue is the
// only failure point in the flow.
type noSubscriptionRepository struct{}

func (noSubscriptionRepository) LoadActive(context.Context, uuid.UUID) (payment.Subscription, bool, error) {
	return payment.Subscription{}, false, nil
}

func (noSubscriptionRepository) LoadDeliveryTarget(context.Context, uuid.UUID) (string, string, error) {
	return "", "", errors.New("no active subscription: LoadDeliveryTarget must not be called")
}

// unusedDeliveryRepository fails the test if the flow ever reaches delivery
// creation, which it must not with no active subscription.
type unusedDeliveryRepository struct{ t *testing.T }

func (r unusedDeliveryRepository) Create(context.Context, payment.DeliveryDraft) (uuid.UUID, error) {
	r.t.Helper()
	r.t.Errorf("DeliveryRepository.Create was called although there is no active subscription")
	return uuid.Nil, errors.New("delivery repository must not be called")
}

func (r unusedDeliveryRepository) FindByID(context.Context, uuid.UUID) (payment.Delivery, error) {
	r.t.Helper()
	r.t.Errorf("DeliveryRepository.FindByID was called although there is no active subscription")
	return payment.Delivery{}, errors.New("delivery repository must not be called")
}

func (r unusedDeliveryRepository) RecordAttempt(context.Context, uuid.UUID, payment.DeliveryStatus, int, time.Time) error {
	r.t.Helper()
	r.t.Errorf("DeliveryRepository.RecordAttempt was called although there is no active subscription")
	return errors.New("delivery repository must not be called")
}

// recordingIDGenerator hands out real UUIDv7 values and remembers them, so
// the test can look for exactly the identifiers the use case minted.
type recordingIDGenerator struct {
	inner  clock.UUIDv7Generator
	issued []uuid.UUID
}

func (g *recordingIDGenerator) NewID() uuid.UUID {
	id := g.inner.NewID()
	g.issued = append(g.issued, id)
	return id
}

// TestCreatePayment_FailingOutboxEnqueue_RollsBackEverything is Step 5.3, and
// the reason it runs against a real database: a fake TxManager could "roll
// back" by simply never writing anything, which proves nothing about whether
// the transaction actually spans the payment INSERT and the outbox write
// (spec §4.1, ADR-0008).
//
// The use case is wired with the real TxManager, PaymentRepository and
// IdempotencyStore, and an OutboxWriter that always fails. The token is
// card_processing_approved so the payment is created PROCESSING, which is
// what triggers the SETTLE_PAYMENT enqueue that fails.
//
// On the idempotency side, note what is *not* asserted. Claim commits
// independently of the work transaction on purpose (ADR-0007: a
// same-transaction claim would roll back on a real crash, leaving nothing for
// the lease to reclaim), so the rollback does not delete or reset the record —
// it stays IN_FLIGHT with its original claimed_at until the lease expires.
// What must hold is that it was never falsely COMPLETED: Complete sits after
// the enqueue inside the same transaction and was never reached, so a later
// request with this key sees IN_FLIGHT and conflicts, rather than replaying a
// response for a payment that does not exist.
func TestCreatePayment_FailingOutboxEnqueue_RollsBackEverything(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_atomicity", now)
	require.NoError(t, err)

	payments := NewPaymentRepository(pool)
	idempotency := NewIdempotencyStore(pool)
	outbox := &failingOutboxWriter{pool: pool, err: errOutboxUnavailable}
	ids := &recordingIDGenerator{}

	uc := payment.NewCreatePaymentUseCase(payment.CreatePaymentDeps{
		Tx:               NewTxManager(pool),
		Payments:         payments,
		Idempotency:      idempotency,
		Outbox:           outbox,
		Subscriptions:    noSubscriptionRepository{},
		Deliveries:       unusedDeliveryRepository{t: t},
		Clock:            clock.NewFake(now),
		IDs:              ids,
		ProcessingDelay:  3 * time.Second,
		IdempotencyLease: 30 * time.Second,
	})

	amount, err := payment.NewAmount(10990)
	require.NoError(t, err)

	const key = "idem-atomicity-1"
	req := payment.CreatePaymentRequest{
		AccountID:      accountID,
		IdempotencyKey: key,
		ReferenceID:    "checkout:123",
		Amount:         amount,
		Currency:       payment.CurrencyBRL,
		Token:          payment.TokenCardProcessingApproved,
	}
	encode := func(payment.Payment) (int, []byte) {
		return 201, []byte(`{"status":"PROCESSING"}`)
	}

	_, err = uc.Execute(ctx, req, encode)
	require.Error(t, err)
	require.ErrorIs(t, err, errOutboxUnavailable)

	// The failure landed where the test intends it to: on the settlement
	// enqueue, with the payment row already inserted inside the transaction.
	require.Equal(t, 1, outbox.calls)
	require.NoError(t, outbox.readErr)
	require.Equal(t, 1, outbox.paymentsVisibleInTx,
		"the payment INSERT must have happened inside the transaction before the outbox failure, otherwise this test proves nothing about rollback")

	// The identifiers the use case minted: the payment's, then the provider
	// transaction's. subjectID confirms which one the payment got without
	// relying on argument evaluation order.
	require.Len(t, ids.issued, 2)
	paymentID := outbox.subjectID
	require.Equal(t, ids.issued[0], paymentID)

	// (a) The payment was rolled back, not left behind.
	_, err = payments.FindByID(ctx, paymentID)
	require.ErrorIs(t, err, payment.ErrPaymentNotFound)

	var total int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM payments`).Scan(&total))
	require.Equal(t, 0, total, "no payment row of any id may survive the rolled-back transaction")

	// Nor did anything else the transaction would have written survive.
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM outbox_work`).Scan(&total))
	require.Equal(t, 0, total)

	// (b) The idempotency record is still IN_FLIGHT under its original claim,
	// and Complete never ran.
	rec, err := idempotency.Load(ctx, accountID, key)
	require.NoError(t, err)
	require.Equal(t, payment.IdempotencyInFlight, rec.State)
	require.Equal(t, uuid.Nil, rec.PaymentID)
	require.Equal(t, 0, rec.ResponseStatus)
	require.Nil(t, rec.ResponseBody)
	require.True(t, now.Equal(rec.ClaimedAt),
		"the claim predates the transaction and is untouched by its rollback: want %s, got %s", now, rec.ClaimedAt)
	require.True(t, rec.CompletedAt.IsZero())
}
