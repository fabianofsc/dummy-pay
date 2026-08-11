package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedAccount upserts the technical account identified by keyID. candidateID
// is used only if no account with this keyID exists yet; if one already
// exists, its stored id is returned instead — SeedAccount never creates a
// second row for the same keyID (spec §3).
func SeedAccount(ctx context.Context, pool *pgxpool.Pool, candidateID uuid.UUID, keyID string, now time.Time) (uuid.UUID, error) {
	// Use INSERT ... ON CONFLICT ... DO UPDATE with a no-op update to ensure
	// RETURNING works for both the insert (new row) and conflict (existing row)
	// cases. PostgreSQL does not support DO NOTHING ... RETURNING, so we use
	// DO UPDATE SET key_id = EXCLUDED.key_id as a standard no-op that still
	// allows RETURNING to emit the row — either the newly inserted one or the
	// existing one that caused the conflict.
	var id uuid.UUID
	err := pool.QueryRow(
		ctx,
		`INSERT INTO accounts (id, key_id, created_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (key_id) DO UPDATE SET key_id = EXCLUDED.key_id
		 RETURNING id`,
		candidateID, keyID, now,
	).Scan(&id)
	if err != nil {
		return uuid.UUID{}, err
	}
	return id, nil
}
