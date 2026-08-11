-- +goose Up
CREATE TABLE accounts (
    id          uuid PRIMARY KEY,
    key_id      text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL
);

-- +goose Down
DROP TABLE accounts;
