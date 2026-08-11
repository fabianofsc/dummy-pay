-- +goose Up
CREATE TABLE payments (
    id                      uuid PRIMARY KEY,
    account_id              uuid NOT NULL REFERENCES accounts(id),
    reference_id            text NOT NULL,
    amount_cents            bigint NOT NULL CHECK (amount_cents > 0),
    currency                text NOT NULL CHECK (currency = 'BRL'),
    payment_token           text NOT NULL,
    status                  text NOT NULL CHECK (status IN ('APPROVED','REJECTED','PROCESSING')),
    provider_transaction_id uuid NOT NULL UNIQUE,
    created_at              timestamptz NOT NULL,
    updated_at              timestamptz NOT NULL
);

-- +goose Down
DROP TABLE payments;
