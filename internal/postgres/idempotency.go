package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/payment"
)

// IdempotencyStore is the Postgres adapter for payment.IdempotencyStore
// (spec §3 "idempotency_keys", §4.1, ADR-0007).
type IdempotencyStore struct {
	pool *pgxpool.Pool
}

// NewIdempotencyStore constructs an IdempotencyStore against pool.
func NewIdempotencyStore(pool *pgxpool.Pool) *IdempotencyStore {
	return &IdempotencyStore{pool: pool}
}

// Claim attempts to take ownership of (accountID, key) with a single
// INSERT ... ON CONFLICT DO NOTHING RETURNING. The primary key is the
// concurrency control (ADR-0007): the database, not this process, decides who
// won, so the guarantee holds across processes.
//
// A conflict is signalled by pgx.ErrNoRows — DO NOTHING suppresses the insert
// and therefore emits no RETURNING row — and is reported as ok=false with a
// nil error, because losing a race is the expected outcome, not a failure.
// Every other error is returned unchanged so a genuine fault (connection lost,
// FK violation on account_id) can never be mistaken for a lost race.
func (s *IdempotencyStore) Claim(ctx context.Context, accountID uuid.UUID, key string, fingerprint [32]byte, claimedAt time.Time) (bool, error) {
	var claimedBy uuid.UUID
	err := querier(ctx, s.pool).QueryRow(ctx,
		`INSERT INTO idempotency_keys
			(account_id, idempotency_key, request_fingerprint, state, claimed_at)
		 VALUES ($1, $2, $3, 'IN_FLIGHT', $4)
		 ON CONFLICT (account_id, idempotency_key) DO NOTHING
		 RETURNING account_id`,
		accountID, key, fingerprint[:], claimedAt,
	).Scan(&claimedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Load reads the record for (accountID, key), mapping pgx.ErrNoRows to
// payment.ErrIdempotencyRecordNotFound.
//
// payment_id, response_status, response_body and completed_at are NULL until
// Complete runs, so each is scanned through a type that can carry NULL and
// then converted, leaving the domain field at its zero value when the column
// is unset. This is not defensive habit: scanning a NULL straight into an int
// or a time.Time fails outright with "cannot scan NULL into *int", so every
// Load of an IN_FLIGHT record would error. (A NULL uuid does survive a bare
// uuid.UUID, because google/uuid implements sql.Scanner and reads NULL as
// uuid.Nil — but it is scanned through pgtype here anyway, so all four
// nullable columns are handled one way rather than three explicitly and one
// by coincidence.) The payment repository needs none of this: its columns are
// all NOT NULL.
func (s *IdempotencyStore) Load(ctx context.Context, accountID uuid.UUID, key string) (payment.IdempotencyRecord, error) {
	row := querier(ctx, s.pool).QueryRow(ctx,
		`SELECT account_id, idempotency_key, request_fingerprint, state,
		        payment_id, response_status, response_body, claimed_at, completed_at
		 FROM idempotency_keys
		 WHERE account_id = $1 AND idempotency_key = $2`,
		accountID, key,
	)

	var (
		rec            payment.IdempotencyRecord
		fingerprint    []byte
		state          string
		paymentID      pgtype.UUID
		responseStatus pgtype.Int4
		completedAt    pgtype.Timestamptz
	)
	// response_body scans into rec.ResponseBody directly: a NULL bytea yields
	// a nil []byte, which is already the zero value the port documents.
	err := row.Scan(&rec.AccountID, &rec.IdempotencyKey, &fingerprint, &state,
		&paymentID, &responseStatus, &rec.ResponseBody, &rec.ClaimedAt, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return payment.IdempotencyRecord{}, payment.ErrIdempotencyRecordNotFound
	}
	if err != nil {
		return payment.IdempotencyRecord{}, err
	}

	// request_fingerprint is a bytea with no length constraint, so a row
	// written by anything other than Claim could be the wrong size. Reject it
	// rather than let the array conversion panic.
	if len(fingerprint) != len(rec.RequestFingerprint) {
		return payment.IdempotencyRecord{}, fmt.Errorf(
			"postgres: request_fingerprint for idempotency key %q is %d bytes, want %d",
			key, len(fingerprint), len(rec.RequestFingerprint))
	}
	rec.RequestFingerprint = [32]byte(fingerprint)
	rec.State = payment.IdempotencyState(state)
	if paymentID.Valid {
		rec.PaymentID = uuid.UUID(paymentID.Bytes)
	}
	if responseStatus.Valid {
		rec.ResponseStatus = int(responseStatus.Int32)
	}
	if completedAt.Valid {
		rec.CompletedAt = completedAt.Time
	}

	return rec, nil
}

// Complete marks the record COMPLETED and stores the exact response bytes a
// replay must return. responseBody is written as bytea, not jsonb, so a replay
// reproduces the original response byte for byte rather than a
// re-serialisation of it (spec §3).
//
// Matching no row is reported as payment.ErrIdempotencyRecordNotFound: a
// silent no-op here would leave the caller believing a replayable response was
// recorded when none was.
func (s *IdempotencyStore) Complete(ctx context.Context, accountID uuid.UUID, key string, paymentID uuid.UUID, responseStatus int, responseBody []byte, completedAt time.Time) error {
	tag, err := querier(ctx, s.pool).Exec(ctx,
		`UPDATE idempotency_keys
		 SET state = 'COMPLETED', payment_id = $3, response_status = $4,
		     response_body = $5, completed_at = $6
		 WHERE account_id = $1 AND idempotency_key = $2`,
		accountID, key, paymentID, responseStatus, responseBody, completedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return payment.ErrIdempotencyRecordNotFound
	}
	return nil
}

// Reclaim takes over an abandoned IN_FLIGHT record: a conditional UPDATE that
// matches only while state is still IN_FLIGHT and claimed_at is strictly
// before cutoff (spec §4.1, "Reclaiming an expired lease is a conditional
// update"). Two requests racing to reclaim the same key therefore resolve the
// way the original claim did — concurrent UPDATEs against one row serialise,
// the loser re-evaluates its WHERE clause against the row the winner just
// wrote, and since the new claimed_at is later than cutoff it no longer
// matches. One winner, decided by the database (ADR-0007).
//
// The comparison is strict: a row claimed at exactly cutoff is inside its
// lease, not past it. Losing is reported as ok=false with a nil error, exactly
// as in Claim above.
//
// Complete deliberately carries no ownership token, and the obvious race — a
// slow original owner completing a key someone else has already reclaimed,
// overwriting the reclaimer's payment and response with stale data — cannot
// occur here by construction. Spec §4.1 puts Claim, the work, and Complete in
// one transaction. Under READ COMMITTED a row inserted by an open, uncommitted
// transaction is invisible to every other transaction, so while that
// transaction runs no other process can see the IN_FLIGHT row at all, let
// alone reclaim it. A row only becomes reclaimable if the transaction that
// claimed it committed the claim and never completed it, which happens only
// when the owning process died — and a dead process never calls Complete. A
// live process completing outside the transaction that claimed would itself
// violate spec §4.1; that is the atomicity guarantee's job to enforce, not
// something to patch around with a token here.
func (s *IdempotencyStore) Reclaim(ctx context.Context, accountID uuid.UUID, key string, cutoff, now time.Time) (bool, error) {
	var reclaimedBy uuid.UUID
	err := querier(ctx, s.pool).QueryRow(ctx,
		`UPDATE idempotency_keys
		 SET claimed_at = $3
		 WHERE account_id = $1 AND idempotency_key = $2
		   AND state = 'IN_FLIGHT' AND claimed_at < $4
		 RETURNING account_id`,
		accountID, key, now, cutoff,
	).Scan(&reclaimedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
