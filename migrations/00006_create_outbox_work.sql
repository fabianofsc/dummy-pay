-- +goose Up
CREATE TABLE outbox_work (
    id          uuid PRIMARY KEY,
    kind        text NOT NULL CHECK (kind IN ('SETTLE_PAYMENT','DELIVER_WEBHOOK')),
    subject_id  uuid NOT NULL,
    due_at      timestamptz NOT NULL,
    state       text NOT NULL CHECK (state IN ('PENDING','DONE','FAILED')),
    claimed_at  timestamptz,
    last_error  text,
    created_at  timestamptz NOT NULL
);

CREATE INDEX outbox_work_pending_due_idx ON outbox_work (state, due_at)
    WHERE state = 'PENDING';

-- +goose Down
DROP TABLE outbox_work;
