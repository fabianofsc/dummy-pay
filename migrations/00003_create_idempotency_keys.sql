-- +goose Up
CREATE TABLE idempotency_keys (
    account_id           uuid NOT NULL REFERENCES accounts(id),
    idempotency_key      text NOT NULL,
    request_fingerprint  bytea NOT NULL,
    state                text NOT NULL CHECK (state IN ('IN_FLIGHT','COMPLETED')),
    payment_id           uuid REFERENCES payments(id),
    response_status      integer,
    response_body        bytea,
    claimed_at            timestamptz NOT NULL,
    completed_at          timestamptz,
    PRIMARY KEY (account_id, idempotency_key)
);

-- +goose Down
DROP TABLE idempotency_keys;
