package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/payment"
	"dummypay/internal/webhook"
)

// uniqueViolationCode is the PostgreSQL error code for a unique constraint
// violation (23505), used to detect the partial unique index on
// webhook_subscriptions(account_id) WHERE active rejecting a second active
// subscription (spec §3, ADR-0009).
const uniqueViolationCode = "23505"

// ErrSubscriptionExists aliases payment.ErrSubscriptionExists: Create
// returns the domain sentinel directly, so callers branch on it without this
// package needing its own copy. Kept as a name here too, unchanged, so
// existing call sites and tests referencing postgres.ErrSubscriptionExists
// need no update.
var ErrSubscriptionExists = payment.ErrSubscriptionExists

// SubscriptionRepository is the Postgres adapter for
// payment.SubscriptionRepository, plus subscription creation (spec §3
// "webhook_subscriptions", §4.2, ADR-0009).
type SubscriptionRepository struct {
	pool   *pgxpool.Pool
	encKey []byte
}

// NewSubscriptionRepository constructs a SubscriptionRepository against
// pool, encrypting secrets under encKey (must be 32 bytes, AES-256).
func NewSubscriptionRepository(pool *pgxpool.Pool, encKey []byte) *SubscriptionRepository {
	return &SubscriptionRepository{pool: pool, encKey: encKey}
}

// Create encrypts secretPlaintext and inserts a new active subscription. A
// second active subscription for the same account is rejected by the
// database's partial unique index, surfaced here as ErrSubscriptionExists
// rather than an application-level pre-check (spec §4.2).
func (r *SubscriptionRepository) Create(ctx context.Context, id, accountID uuid.UUID, url string, events []payment.EventType, secretPlaintext string, createdAt time.Time) error {
	ciphertext, nonce, err := webhook.EncryptSecret(r.encKey, []byte(secretPlaintext))
	if err != nil {
		return err
	}

	eventStrings := make([]string, len(events))
	for i, e := range events {
		eventStrings[i] = string(e)
	}

	_, err = querier(ctx, r.pool).Exec(ctx,
		`INSERT INTO webhook_subscriptions
			(id, account_id, url, events, secret_ciphertext, secret_nonce, active, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, true, $7)`,
		id, accountID, url, eventStrings, ciphertext, nonce, createdAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return ErrSubscriptionExists
		}
		return err
	}
	return nil
}

// LoadActive loads the account's active subscription, reduced to what the
// create-payment flow needs (payment.SubscriptionRepository). ok is false
// when there is no active subscription.
func (r *SubscriptionRepository) LoadActive(ctx context.Context, accountID uuid.UUID) (payment.Subscription, bool, error) {
	var (
		id           uuid.UUID
		eventStrings []string
	)
	err := querier(ctx, r.pool).QueryRow(ctx,
		`SELECT id, events FROM webhook_subscriptions WHERE account_id = $1 AND active`,
		accountID,
	).Scan(&id, &eventStrings)
	if errors.Is(err, pgx.ErrNoRows) {
		return payment.Subscription{}, false, nil
	}
	if err != nil {
		return payment.Subscription{}, false, err
	}

	events := make([]payment.EventType, len(eventStrings))
	for i, e := range eventStrings {
		events[i] = payment.EventType(e)
	}

	return payment.Subscription{ID: id, Events: events}, true, nil
}

// LoadDeliveryTarget loads the URL and decrypted secret for the subscription
// id, the data the worker's DELIVER_WEBHOOK handler needs to send and sign a
// delivery (spec §5, ADR-0009). Decryption happens here, at the boundary
// that already holds the encryption key — the domain never sees ciphertext
// or the key, only the plaintext secret this returns.
func (r *SubscriptionRepository) LoadDeliveryTarget(ctx context.Context, id uuid.UUID) (url, secret string, err error) {
	var ciphertext, nonce []byte
	err = querier(ctx, r.pool).QueryRow(ctx,
		`SELECT url, secret_ciphertext, secret_nonce FROM webhook_subscriptions WHERE id = $1`,
		id,
	).Scan(&url, &ciphertext, &nonce)
	if err != nil {
		return "", "", err
	}

	plaintext, err := webhook.DecryptSecret(r.encKey, ciphertext, nonce)
	if err != nil {
		return "", "", err
	}

	return url, string(plaintext), nil
}
