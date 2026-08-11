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
// The update is conditional on `claimed_at = $7`, which makes claimedAt an
// ownership token rather than a stored value: the caller passes the claim it
// believes it holds, and the write happens only while the row still carries
// it. This is the counterweight to Claim committing outside the work
// transaction (see Reclaim). Without it a request whose work outran its lease
// would overwrite the row of whoever reclaimed the key in the meantime,
// leaving two payments behind one idempotency key and a stored response that
// was never returned to anybody — a replay would then hand that response to a
// caller as though it were the original.
//
// Matching no row is never silent: a Complete that reports success without
// writing would leave the caller believing a replayable response was
// recorded. The two ways to match nothing are told apart deliberately, because
// they mean opposite things to whoever reads the log — a missing record is a
// bug in the calling code, while a lost claim is a lease that wants to be
// longer, or a request that wants to be faster.
func (s *IdempotencyStore) Complete(ctx context.Context, accountID uuid.UUID, key string, claimedAt time.Time, paymentID uuid.UUID, responseStatus int, responseBody []byte, completedAt time.Time) error {
	tag, err := querier(ctx, s.pool).Exec(ctx,
		`UPDATE idempotency_keys
		 SET state = 'COMPLETED', payment_id = $3, response_status = $4,
		     response_body = $5, completed_at = $6
		 WHERE account_id = $1 AND idempotency_key = $2
		   AND state = 'IN_FLIGHT' AND claimed_at = $7`,
		accountID, key, paymentID, responseStatus, responseBody, completedAt, claimedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return s.whyCompleteMatchedNothing(ctx, accountID, key)
	}
	return nil
}

// whyCompleteMatchedNothing distinguishes "no such record" from "the record
// moved on without you". One extra read is acceptable here because it runs
// only on a path that has already failed, and the answer changes what an
// operator should do about it.
//
// A record that exists but did not match is reported as ownership lost —
// whether it is IN_FLIGHT under someone else's claim or already COMPLETED,
// the caller no longer holds the right to write it.
func (s *IdempotencyStore) whyCompleteMatchedNothing(ctx context.Context, accountID uuid.UUID, key string) error {
	if _, err := s.Load(ctx, accountID, key); err != nil {
		// Includes payment.ErrIdempotencyRecordNotFound, which is the answer
		// when nothing was ever claimed under this key.
		return err
	}
	return payment.ErrIdempotencyOwnershipLost
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
// Reclaim only has anything to do because Claim commits on its own, before
// and outside the transaction that does the work. That ordering is the whole
// point of the lease: were the claim part of the work transaction, a process
// dying mid-flight would roll its claim back along with everything else,
// leaving no IN_FLIGHT row for anyone to reclaim and no abandoned key for the
// lease to recover (ADR-0007).
//
// The price is that "reclaimable" and "abandoned" are not the same thing. A
// live but slow owner — a long GC pause, a stalled query, a lease configured
// shorter than the work takes — is indistinguishable from a dead one from
// here, so its key can be reclaimed out from under it while it is still
// working. Nothing in this function can prevent that, and it should not try:
// the lease is a timeout, and timeouts are sometimes wrong. What must not
// happen is the slow owner then finishing and overwriting the reclaimer's
// work, and that is refused by Complete's claimed_at token, not here.
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
