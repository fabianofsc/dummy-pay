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

// ErrSubscriptionExists is returned by Create when the account already has
// an active subscription. The database's partial unique index is what
// actually enforces this — this error just names the pgconn.PgError it
// produces (spec §4.2, 409 subscription_exists).
var ErrSubscriptionExists = errors.New("account already has an active subscription")

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
