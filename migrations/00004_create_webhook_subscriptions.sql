-- +goose Up
CREATE TABLE webhook_subscriptions (
    id                uuid PRIMARY KEY,
    account_id        uuid NOT NULL REFERENCES accounts(id),
    url               text NOT NULL,
    events            text[] NOT NULL,
    secret_ciphertext bytea NOT NULL,
    secret_nonce      bytea NOT NULL,
    active            boolean NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL
);

CREATE UNIQUE INDEX webhook_subscriptions_account_active_idx
    ON webhook_subscriptions (account_id) WHERE active;

-- +goose Down
DROP TABLE webhook_subscriptions;
