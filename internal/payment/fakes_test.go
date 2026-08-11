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
	// completeLosesOwnership forces Complete to report
	// ErrIdempotencyOwnershipLost, simulating this caller's lease expiring
	// and the key being reclaimed while its work transaction was still
	// running.
	completeLosesOwnership bool
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

func (s *fakeIdempotencyStore) Complete(_ context.Context, accountID uuid.UUID, key string, claimedAt time.Time, paymentID uuid.UUID, responseStatus int, responseBody []byte, completedAt time.Time) error {
	s.completeCalls++
	if s.completeLosesOwnership {
		return ErrIdempotencyOwnershipLost
	}
	k := idempotencyKey{accountID, key}
	rec, ok := s.records[k]
	if !ok {
		return ErrIdempotencyRecordNotFound
	}
	if rec.State != IdempotencyInFlight {
		return errors.New("fake idempotency store: complete on a record that is not IN_FLIGHT")
	}
	// The same ownership check the real store makes in SQL: the caller must
	// still hold the claim it is completing.
	if !rec.ClaimedAt.Equal(claimedAt) {
		return ErrIdempotencyOwnershipLost
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

	// deliveryTargetURL/deliveryTargetSecret are what LoadDeliveryTarget
	// returns, keyed only by call — tests using this fake exercise one
	// subscription at a time.
	deliveryTargetURL    string
	deliveryTargetSecret string
}

func (r *fakeSubscriptionRepository) LoadActive(_ context.Context, _ uuid.UUID) (Subscription, bool, error) {
	if !r.active {
		return Subscription{}, false, nil
	}
	return r.sub, true, nil
}

func (r *fakeSubscriptionRepository) LoadDeliveryTarget(_ context.Context, _ uuid.UUID) (string, string, error) {
	return r.deliveryTargetURL, r.deliveryTargetSecret, nil
}

type fakeDeliveryRepository struct {
	drafts []DeliveryDraft
	ids    []uuid.UUID

	// byID backs FindByID/RecordAttempt so the worker's DELIVER_WEBHOOK
	// handler can be tested the same way the real repository would behave:
	// Create seeds the row, RecordAttempt mutates it in place.
	byID map[uuid.UUID]*Delivery

	// recordAttemptCalls counts RecordAttempt invocations, so a test can
	// assert attempt tracking happened exactly once per Send.
	recordAttemptCalls int
}

func (r *fakeDeliveryRepository) Create(_ context.Context, d DeliveryDraft) (uuid.UUID, error) {
	id := uuid.New()
	r.drafts = append(r.drafts, d)
	r.ids = append(r.ids, id)
	if r.byID == nil {
		r.byID = map[uuid.UUID]*Delivery{}
	}
	r.byID[id] = &Delivery{
		ID:             id,
		SubscriptionID: d.SubscriptionID,
		PaymentID:      d.PaymentID,
		EventID:        d.EventID,
		EventType:      d.EventType,
		Payload:        []byte(`{"fake":"payload"}`),
		Status:         DeliveryPending,
		CreatedAt:      d.CreatedAt,
	}
	return id, nil
}

// seedDelivery installs a delivery row directly, for tests that exercise
// DELIVER_WEBHOOK without going through Create first.
func (r *fakeDeliveryRepository) seedDelivery(d Delivery) {
	if r.byID == nil {
		r.byID = map[uuid.UUID]*Delivery{}
	}
	cp := d
	r.byID[d.ID] = &cp
}

func (r *fakeDeliveryRepository) FindByID(_ context.Context, id uuid.UUID) (Delivery, error) {
	d, ok := r.byID[id]
	if !ok {
		return Delivery{}, errors.New("fake delivery repository: not found")
	}
	return *d, nil
}

func (r *fakeDeliveryRepository) RecordAttempt(_ context.Context, id uuid.UUID, status DeliveryStatus, httpStatus int, attemptedAt time.Time) error {
	r.recordAttemptCalls++
	d, ok := r.byID[id]
	if !ok {
		return errors.New("fake delivery repository: not found")
	}
	d.AttemptCount++
	d.Status = status
	d.LastAttemptedAt = attemptedAt
	d.LastHTTPStatus = httpStatus
	return nil
}

// fakeOutboxClaimer holds ClaimedWork items seeded directly by a test,
// standing in for the real database's FOR UPDATE SKIP LOCKED claim
// (proved separately, against a real database, in Step 9.1). Claiming here
// removes the item so a second ClaimDue call in the same test — modelling
// "running the same work twice" — sees nothing left to claim, exactly as
// outbox_work's claim-to-DONE transition does.
// fakeSender stands in for the HTTP delivery adapter (proved for real,
// against httptest.Server, in Step 8.2). result/err are what the next Send
// call returns; onSend, if set, runs first — used to assert ordering, e.g.
// that the delivery row already existed when Send was reached.
type fakeSender struct {
	result  int
	err     error
	calls   int
	onSend  func()
	gotURL  string
	gotBody []byte
}

func (s *fakeSender) Send(_ context.Context, url string, body []byte, _ string) (int, error) {
	s.calls++
	s.gotURL = url
	s.gotBody = body
	if s.onSend != nil {
		s.onSend()
	}
	return s.result, s.err
}

type fakeOutboxClaimer struct {
	pending []ClaimedWork
	calls   int
}

func (c *fakeOutboxClaimer) ClaimDue(_ context.Context, _ time.Time, batch int) ([]ClaimedWork, error) {
	c.calls++
	if len(c.pending) == 0 {
		return nil, nil
	}
	n := batch
	if n > len(c.pending) {
		n = len(c.pending)
	}
	claimed := c.pending[:n]
	c.pending = c.pending[n:]
	return claimed, nil
}
