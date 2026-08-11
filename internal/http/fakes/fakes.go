// Package fakes provides test doubles for HTTP layer testing.
package fakes

import (
	"context"
	"time"

	"github.com/google/uuid"

	"dummypay/internal/payment"
)

type PaymentRepository struct {
	accountID uuid.UUID
	payments  map[uuid.UUID]*payment.Payment
}

func NewPaymentRepository() *PaymentRepository {
	return &PaymentRepository{
		payments: make(map[uuid.UUID]*payment.Payment),
	}
}

func (r *PaymentRepository) SeedAccountID(id uuid.UUID) uuid.UUID {
	r.accountID = id
	return id
}

func (r *PaymentRepository) Insert(ctx context.Context, p payment.Payment) error {
	r.payments[p.ID] = &p
	return nil
}

func (r *PaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (payment.Payment, error) {
	if p, ok := r.payments[id]; ok {
		return *p, nil
	}
	return payment.Payment{}, payment.ErrPaymentNotFound
}

func (r *PaymentRepository) Update(ctx context.Context, id uuid.UUID, status payment.Status) error {
	p, ok := r.payments[id]
	if !ok {
		return payment.ErrPaymentNotFound
	}
	p.Status = status
	p.UpdatedAt = time.Now()
	return nil
}

type IdempotencyStore struct {
	records map[string]*payment.IdempotencyRecord
}

func NewIdempotencyStore() *IdempotencyStore {
	return &IdempotencyStore{
		records: make(map[string]*payment.IdempotencyRecord),
	}
}

func (s *IdempotencyStore) Claim(ctx context.Context, accountID uuid.UUID, key string, fingerprint [32]byte, now time.Time) (payment.IdempotencyRecord, bool, error) {
	mapKey := accountID.String() + ":" + key
	if rec, ok := s.records[mapKey]; ok {
		return *rec, false, nil
	}
	rec := payment.IdempotencyRecord{
		AccountID:          accountID,
		IdempotencyKey:     key,
		RequestFingerprint: fingerprint,
		State:              payment.IdempotencyInFlight,
		ClaimedAt:          now,
	}
	s.records[mapKey] = &rec
	return rec, true, nil
}

func (s *IdempotencyStore) Complete(ctx context.Context, accountID uuid.UUID, key string, paymentID uuid.UUID, status int, body []byte) error {
	mapKey := accountID.String() + ":" + key
	rec, ok := s.records[mapKey]
	if !ok {
		return payment.ErrIdempotencyRecordNotFound
	}
	rec.State = payment.IdempotencyCompleted
	rec.PaymentID = paymentID
	rec.ResponseStatus = status
	rec.ResponseBody = body
	rec.CompletedAt = time.Now()
	return nil
}

func (s *IdempotencyStore) Load(ctx context.Context, accountID uuid.UUID, key string) (payment.IdempotencyRecord, error) {
	mapKey := accountID.String() + ":" + key
	if rec, ok := s.records[mapKey]; ok {
		return *rec, nil
	}
	return payment.IdempotencyRecord{}, payment.ErrIdempotencyRecordNotFound
}

func (s *IdempotencyStore) Reclaim(ctx context.Context, accountID uuid.UUID, key string, leaseExpiry time.Time, now time.Time) (payment.IdempotencyRecord, bool, error) {
	mapKey := accountID.String() + ":" + key
	rec, ok := s.records[mapKey]
	if !ok {
		return payment.IdempotencyRecord{}, false, nil
	}
	if rec.State != payment.IdempotencyInFlight {
		return *rec, false, nil
	}
	if rec.ClaimedAt.After(leaseExpiry) {
		rec.ClaimedAt = now
		return *rec, true, nil
	}
	return *rec, false, nil
}

type OutboxWriter struct{}

func NewOutboxWriter() *OutboxWriter {
	return &OutboxWriter{}
}

func (w *OutboxWriter) Enqueue(ctx context.Context, kind payment.OutboxKind, subjectID uuid.UUID, dueAt time.Time) error {
	return nil
}

type SubscriptionRepository struct{}

func NewSubscriptionRepository() *SubscriptionRepository {
	return &SubscriptionRepository{}
}

func (r *SubscriptionRepository) LoadActive(ctx context.Context, accountID uuid.UUID) (payment.Subscription, bool, error) {
	return payment.Subscription{}, false, nil
}

type DeliveryRepository struct{}

func NewDeliveryRepository() *DeliveryRepository {
	return &DeliveryRepository{}
}

func (r *DeliveryRepository) Create(ctx context.Context, draft payment.DeliveryDraft) (uuid.UUID, error) {
	return uuid.New(), nil
}

type IDGenerator struct {
	counter int
}

func NewIDGenerator() *IDGenerator {
	return &IDGenerator{}
}

func (g *IDGenerator) NewID() uuid.UUID {
	g.counter++
	return uuid.New()
}

type TxManager struct{}

func NewTxManager() *TxManager {
	return &TxManager{}
}

func (m *TxManager) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
