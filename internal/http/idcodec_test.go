package httpapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var allIDKinds = []idKind{
	idKindPayment,
	idKindTransaction,
	idKindSubscription,
	idKindEvent,
	idKindDelivery,
}

func TestEncodeID_ProducesThePrefixedString(t *testing.T) {
	id := uuid.MustParse("0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b")

	require.Equal(t, "pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b", encodeID(idKindPayment, id))
	require.Equal(t, "txn_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b", encodeID(idKindTransaction, id))
	require.Equal(t, "sub_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b", encodeID(idKindSubscription, id))
	require.Equal(t, "evt_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b", encodeID(idKindEvent, id))
	require.Equal(t, "dlv_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b", encodeID(idKindDelivery, id))
}

func TestDecodeID_MatchingPrefix_RoundTrips(t *testing.T) {
	id := uuid.MustParse("0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b")

	for _, kind := range allIDKinds {
		t.Run(kind.prefix, func(t *testing.T) {
			encoded := encodeID(kind, id)

			got, err := decodeID(kind, encoded)

			require.NoError(t, err)
			require.Equal(t, id, got)
		})
	}
}

// The property ADR-0006's compliance section requires: a well-formed UUID
// carrying the wrong prefix is rejected, never silently accepted as a
// different kind of identifier. Checked for every prefix against every
// other prefix.
func TestDecodeID_WrongPrefix_IsRejectedForEveryKind(t *testing.T) {
	id := uuid.MustParse("0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b")

	for _, encodeKind := range allIDKinds {
		for _, decodeKind := range allIDKinds {
			if encodeKind == decodeKind {
				continue
			}
			t.Run(encodeKind.prefix+"_as_"+decodeKind.prefix, func(t *testing.T) {
				encoded := encodeID(encodeKind, id)

				_, err := decodeID(decodeKind, encoded)

				require.Error(t, err)
			})
		}
	}
}

func TestDecodeID_MalformedUUID_IsRejectedForEveryKind(t *testing.T) {
	for _, kind := range allIDKinds {
		t.Run(kind.prefix, func(t *testing.T) {
			_, err := decodeID(kind, kind.prefix+"not-a-uuid")

			require.Error(t, err)
		})
	}
}

func TestDecodeID_EmptyString_IsRejected(t *testing.T) {
	for _, kind := range allIDKinds {
		_, err := decodeID(kind, "")
		require.Error(t, err)
	}
}
