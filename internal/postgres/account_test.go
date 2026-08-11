package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSeedAccountCreatesNewRow(t *testing.T) {
	pool := NewTestDB(t)

	ctx := context.Background()
	candidateID := uuid.New()
	keyID := "test_account_key"
	now := time.Now().UTC()

	// First call should create a new row and return the candidateID.
	id, err := SeedAccount(ctx, pool, candidateID, keyID, now)
	require.NoError(t, err)
	require.Equal(t, candidateID, id)

	// Verify exactly one row exists with this keyID.
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM accounts WHERE key_id = $1", keyID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify the row has the expected id and keyID.
	var storedID uuid.UUID
	var storedKeyID string
	err = pool.QueryRow(ctx, "SELECT id, key_id FROM accounts WHERE key_id = $1", keyID).Scan(&storedID, &storedKeyID)
	require.NoError(t, err)
	require.Equal(t, candidateID, storedID)
	require.Equal(t, keyID, storedKeyID)
}

func TestSeedAccountRepeatWithSameKeyIDReturnsExistingID(t *testing.T) {
	pool := NewTestDB(t)

	ctx := context.Background()
	keyID := "test_account_key_repeat"
	now := time.Now().UTC()

	// First call with candidateID1.
	candidateID1 := uuid.New()
	id1, err := SeedAccount(ctx, pool, candidateID1, keyID, now)
	require.NoError(t, err)
	require.Equal(t, candidateID1, id1)

	// Second call with candidateID2 (different ID, same keyID).
	// Should return id1, not candidateID2, proving the second candidate is discarded.
	candidateID2 := uuid.New()
	id2, err := SeedAccount(ctx, pool, candidateID2, keyID, now)
	require.NoError(t, err)
	require.Equal(t, id1, id2, "second call should return the existing id, not the new candidate")

	// Verify exactly one row exists with this keyID.
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM accounts WHERE key_id = $1", keyID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "should have exactly one row for this keyID, not two")

	// Verify the stored ID is id1, not id2.
	var storedID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM accounts WHERE key_id = $1", keyID).Scan(&storedID)
	require.NoError(t, err)
	require.Equal(t, id1, storedID)
}

func TestSeedAccountDifferentKeyIDsProduceDifferentRows(t *testing.T) {
	pool := NewTestDB(t)

	ctx := context.Background()
	now := time.Now().UTC()

	// First account.
	candidateID1 := uuid.New()
	keyID1 := "test_account_1"
	id1, err := SeedAccount(ctx, pool, candidateID1, keyID1, now)
	require.NoError(t, err)
	require.Equal(t, candidateID1, id1)

	// Second account with different keyID.
	candidateID2 := uuid.New()
	keyID2 := "test_account_2"
	id2, err := SeedAccount(ctx, pool, candidateID2, keyID2, now)
	require.NoError(t, err)
	require.Equal(t, candidateID2, id2)

	// Verify two rows exist, one for each keyID.
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM accounts").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Verify they have different IDs.
	require.NotEqual(t, id1, id2)
}
