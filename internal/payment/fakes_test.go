package payment

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Hand-written in-memory fakes for the ports in spec §8, used by the
// use-case tests (spec §10: no database, no HTTP, no mocking library).

type fakeTxManager struct {
	calls int
}

func (m *fakeTxManager) Within(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	return fn(ctx)
}

type fakePaymentRepository struct {
	// inserted records every Insert call in order. It is the only evidence
	// the tests have that the use case did — or did not — create a payment,
	// so payments that existed *before* the call under test are seeded
	// separately and never appear here.
	inserted []Payment
	seeded   []Payment
}

// seed makes p loadable without it counting as an Insert, so a replay test
// can assert that inserted stayed empty.
func (r *fakePaymentRepository) seed(p Payment) {
	r.seeded = append(r.seeded, p)
}

func (r *fakePaymentRepository) Insert(_ context.Context, p Payment) error {
	r.inserted = append(r.inserted, p)
	return nil
}

func (r *fakePaymentRepository) FindByID(_ context.Context, id uuid.UUID) (Payment, error) {
	for _, p := range r.seeded {
		if p.ID == id {
			return p, nil
		}
	}
	for _, p := range r.inserted {
		if p.ID == id {
			return p, nil
		}
	}
	return Payment{}, ErrPaymentNotFound
}

func (r *fakePaymentRepository) Update(_ context.Context, p Payment) error {
	for i, existing := range r.seeded {
		if existing.ID == p.ID {
			r.seeded[i] = p
			return nil
		}
	}
	for i, existing := range r.inserted {
		if existing.ID == p.ID {
			r.inserted[i] = p
			return nil
		}
	}
	return ErrPaymentNotFound
}

type idempotencyKey struct {
	accountID uuid.UUID
	key       string
}

type fakeIdempotencyStore struct {
	records map[idempotencyKey]IdempotencyRecord

	// completeCalls counts Complete calls, so a replay test can prove the
	// owner flow was never entered.
	completeCalls int
	// reclaimCalls counts Reclaim calls.
	reclaimCalls int
	// reclaimLoses forces Reclaim to report ok=false, simulating another
	// process taking over the abandoned key first (spec §4.1).
	reclaimLoses bool
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{records: map[idempotencyKey]IdempotencyRecord{}}
}

// seed installs a record as if a previous request had written it, so the
// branch table of spec §4.1 can be exercised without racing a real one.
func (s *fakeIdempotencyStore) seed(rec IdempotencyRecord) {
	s.records[idempotencyKey{rec.AccountID, rec.IdempotencyKey}] = rec
}

func (s *fakeIdempotencyStore) Claim(_ context.Context, accountID uuid.UUID, key string, fingerprint [32]byte, claimedAt time.Time) (bool, error) {
	k := idempotencyKey{accountID, key}
	if _, exists := s.records[k]; exists {
		return false, nil
	}
	s.records[k] = IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
		State:              IdempotencyInFlight,
		ClaimedAt:          claimedAt,
	}
	return true, nil
}

func (s *fakeIdempotencyStore) Load(_ context.Context, accountID uuid.UUID, key string) (IdempotencyRecord, error) {
	rec, ok := s.records[idempotencyKey{accountID, key}]
	if !ok {
		return IdempotencyRecord{}, ErrIdempotencyRecordNotFound
	}
	return rec, nil
}

func (s *fakeIdempotencyStore) Reclaim(_ context.Context, accountID uuid.UUID, key string, cutoff, now time.Time) (bool, error) {
	s.reclaimCalls++
	if s.reclaimLoses {
		return false, nil
	}
	k := idempotencyKey{accountID, key}
	rec, ok := s.records[k]
	if !ok || rec.State != IdempotencyInFlight || !rec.ClaimedAt.Before(cutoff) {
		return false, nil
	}
	rec.ClaimedAt = now
	s.records[k] = rec
	return true, nil
}

func (s *fakeIdempotencyStore) Complete(_ context.Context, accountID uuid.UUID, key string, paymentID uuid.UUID, responseStatus int, responseBody []byte, completedAt time.Time) error {
	s.completeCalls++
	k := idempotencyKey{accountID, key}
	rec, ok := s.records[k]
	if !ok {
		return ErrIdempotencyRecordNotFound
	}
	if rec.State != IdempotencyInFlight {
		return errors.New("fake idempotency store: complete on a record that is not IN_FLIGHT")
	}
	rec.State = IdempotencyCompleted
	rec.PaymentID = paymentID
	rec.ResponseStatus = responseStatus
	rec.ResponseBody = responseBody
	rec.CompletedAt = completedAt
	s.records[k] = rec
	return nil
}

type outboxEntry struct {
	Kind      OutboxKind
	SubjectID uuid.UUID
	DueAt     time.Time
}

type fakeOutboxWriter struct {
	entries []outboxEntry
}

func (w *fakeOutboxWriter) Enqueue(_ context.Context, kind OutboxKind, subjectID uuid.UUID, dueAt time.Time) error {
	w.entries = append(w.entries, outboxEntry{Kind: kind, SubjectID: subjectID, DueAt: dueAt})
	return nil
}

// entriesOfKind returns the entries enqueued with the given kind, in
// enqueue order.
func (w *fakeOutboxWriter) entriesOfKind(kind OutboxKind) []outboxEntry {
	var got []outboxEntry
	for _, e := range w.entries {
		if e.Kind == kind {
			got = append(got, e)
		}
	}
	return got
}

type fakeSubscriptionRepository struct {
	sub    Subscription
	active bool
}

func (r *fakeSubscriptionRepository) LoadActive(_ context.Context, _ uuid.UUID) (Subscription, bool, error) {
	if !r.active {
		return Subscription{}, false, nil
	}
	return r.sub, true, nil
}

type fakeDeliveryRepository struct {
	drafts []DeliveryDraft
	ids    []uuid.UUID
}

func (r *fakeDeliveryRepository) Create(_ context.Context, d DeliveryDraft) (uuid.UUID, error) {
	id := uuid.New()
	r.drafts = append(r.drafts, d)
	r.ids = append(r.ids, id)
	return id, nil
}
