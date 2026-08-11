package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/payment"
	"dummypay/internal/webhook"
)

// DeliveryRepository is the Postgres adapter for payment.DeliveryRepository
// (spec §3 "webhook_deliveries", §8).
type DeliveryRepository struct {
	pool *pgxpool.Pool
	ids  payment.IDGenerator
}

// NewDeliveryRepository constructs a DeliveryRepository against pool,
// minting delivery row identifiers with ids.
func NewDeliveryRepository(pool *pgxpool.Pool, ids payment.IDGenerator) *DeliveryRepository {
	return &DeliveryRepository{pool: pool, ids: ids}
}

// Create builds the exact payload bytes to be stored, signed, and sent —
// once, here, at creation — and inserts a PENDING delivery row. Nothing
// downstream re-serialises (spec §3, spec §6): the worker's send and every
// retry read this same stored payload. Identifiers are prefixed here, at the
// point the payload is built for an external consumer — the same boundary
// treatment the HTTP responses get, just for a different audience
// (spec §1.3, ADR-0006).
func (r *DeliveryRepository) Create(ctx context.Context, d payment.DeliveryDraft) (uuid.UUID, error) {
	id := r.ids.NewID()

	payloadBytes := webhook.BuildPayload(webhook.EventPayload{
		EventID:               "evt_" + d.EventID.String(),
		Type:                  string(d.EventType),
		CreatedAt:             d.CreatedAt,
		PaymentID:             "pay_" + d.PaymentID.String(),
		ReferenceID:           d.ReferenceID,
		Status:                string(d.Status),
		ProviderTransactionID: "txn_" + d.ProviderTransactionID.String(),
	})

	_, err := querier(ctx, r.pool).Exec(ctx,
		`INSERT INTO webhook_deliveries
			(id, subscription_id, payment_id, event_id, event_type, payload, status, attempt_count, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'PENDING', 0, $7)`,
		id, d.SubscriptionID, d.PaymentID, d.EventID, string(d.EventType), payloadBytes, d.CreatedAt,
	)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// FindByID loads a delivery by id, mapping pgx.ErrNoRows to
// payment.ErrDeliveryNotFound.
func (r *DeliveryRepository) FindByID(ctx context.Context, id uuid.UUID) (payment.Delivery, error) {
	var (
		del             payment.Delivery
		eventType       string
		status          string
		lastAttemptedAt *time.Time
		lastHTTPStatus  *int
	)
	err := querier(ctx, r.pool).QueryRow(ctx,
		`SELECT id, subscription_id, payment_id, event_id, event_type, payload, status,
		        attempt_count, last_attempted_at, last_http_status, created_at
		 FROM webhook_deliveries WHERE id = $1`,
		id,
	).Scan(&del.ID, &del.SubscriptionID, &del.PaymentID, &del.EventID, &eventType, &del.Payload,
		&status, &del.AttemptCount, &lastAttemptedAt, &lastHTTPStatus, &del.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return payment.Delivery{}, payment.ErrDeliveryNotFound
	}
	if err != nil {
		return payment.Delivery{}, err
	}

	del.EventType = payment.EventType(eventType)
	del.Status = payment.DeliveryStatus(status)
	if lastAttemptedAt != nil {
		del.LastAttemptedAt = *lastAttemptedAt
	}
	if lastHTTPStatus != nil {
		del.LastHTTPStatus = *lastHTTPStatus
	}

	return del, nil
}

// RecordAttempt increments attempt_count and sets status, last_attempted_at,
// and last_http_status in one write. httpStatus 0 is stored as a genuine SQL
// NULL, not the integer zero, so a transport failure that never got a
// response is distinguishable from a (nonexistent) HTTP status 0 (spec §5).
func (r *DeliveryRepository) RecordAttempt(ctx context.Context, id uuid.UUID, status payment.DeliveryStatus, httpStatus int, attemptedAt time.Time) error {
	var httpStatusArg any
	if httpStatus != 0 {
		httpStatusArg = httpStatus
	}

	tag, err := querier(ctx, r.pool).Exec(ctx,
		`UPDATE webhook_deliveries
		 SET status = $2, attempt_count = attempt_count + 1, last_attempted_at = $3, last_http_status = $4
		 WHERE id = $1`,
		id, string(status), attemptedAt, httpStatusArg,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return payment.ErrDeliveryNotFound
	}
	return nil
}
