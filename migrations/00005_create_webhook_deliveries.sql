-- +goose Up
CREATE TABLE webhook_deliveries (
    id                uuid PRIMARY KEY,
    subscription_id   uuid NOT NULL REFERENCES webhook_subscriptions(id),
    payment_id        uuid NOT NULL REFERENCES payments(id),
    event_id          uuid NOT NULL UNIQUE,
    event_type        text NOT NULL,
    payload           bytea NOT NULL,
    status            text NOT NULL CHECK (status IN ('PENDING','SENT','FAILED')),
    attempt_count     integer NOT NULL DEFAULT 0,
    last_attempted_at timestamptz,
    last_http_status  integer,
    created_at        timestamptz NOT NULL
);

-- +goose Down
DROP TABLE webhook_deliveries;
