package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/payment"
)

// PaymentRepository is the Postgres adapter for payment.PaymentRepository
// (spec §8, §3 "payments").
type PaymentRepository struct {
	pool *pgxpool.Pool
}

// NewPaymentRepository constructs a PaymentRepository against pool.
func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

// Insert persists a new payment. p is assumed already valid — the domain
// constructs Payment only through validating functions — so this issues a
// plain INSERT; the payments table's CHECK constraints are defense in depth
// (spec §3), not something this method needs to pre-validate against.
func (r *PaymentRepository) Insert(ctx context.Context, p payment.Payment) error {
	_, err := querier(ctx, r.pool).Exec(ctx,
		`INSERT INTO payments
			(id, account_id, reference_id, amount_cents, currency, payment_token,
			 status, provider_transaction_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		p.ID, p.AccountID, p.ReferenceID, int64(p.Amount), string(p.Currency),
		string(p.Token), string(p.Status), p.ProviderTransactionID, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

// FindByID loads a payment by id, mapping pgx.ErrNoRows to
// payment.ErrPaymentNotFound.
func (r *PaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (payment.Payment, error) {
	row := querier(ctx, r.pool).QueryRow(ctx,
		`SELECT id, account_id, reference_id, amount_cents, currency, payment_token,
		        status, provider_transaction_id, created_at, updated_at
		 FROM payments
		 WHERE id = $1`,
		id,
	)

	var (
		p        payment.Payment
		amount   int64
		currency string
		token    string
		status   string
	)
	err := row.Scan(&p.ID, &p.AccountID, &p.ReferenceID, &amount, &currency, &token,
		&status, &p.ProviderTransactionID, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return payment.Payment{}, payment.ErrPaymentNotFound
	}
	if err != nil {
		return payment.Payment{}, err
	}

	p.Amount = payment.Amount(amount)
	p.Currency = payment.Currency(currency)
	p.Token = payment.ScenarioToken(token)
	p.Status = payment.Status(status)

	return p, nil
}

// Update persists p's current fields — used after Settle() transitions a
// payment, to write the new status and updated_at.
func (r *PaymentRepository) Update(ctx context.Context, p payment.Payment) error {
	tag, err := querier(ctx, r.pool).Exec(ctx,
		`UPDATE payments
		 SET reference_id = $2, amount_cents = $3, currency = $4, payment_token = $5,
		     status = $6, provider_transaction_id = $7, updated_at = $8
		 WHERE id = $1`,
		p.ID, p.ReferenceID, int64(p.Amount), string(p.Currency), string(p.Token),
		string(p.Status), p.ProviderTransactionID, p.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return payment.ErrPaymentNotFound
	}
	return nil
}
