package httpapi

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// idKind names one of the five identifier prefixes DummyPay's API uses
// (ADR-0006). The prefix exists only here, at the HTTP boundary; domain and
// storage types are uuid.UUID, never a prefixed string.
type idKind struct {
	prefix string
}

var (
	idKindPayment      = idKind{"pay_"}
	idKindTransaction  = idKind{"txn_"}
	idKindSubscription = idKind{"sub_"}
	idKindEvent        = idKind{"evt_"}
	idKindDelivery     = idKind{"dlv_"}
)

var errInvalidID = errors.New("invalid identifier")

// encodeID renders id with kind's prefix, e.g. "pay_0199a1f4-...".
func encodeID(kind idKind, id uuid.UUID) string {
	return kind.prefix + id.String()
}

// decodeID parses s as an identifier of the given kind. A well-formed UUID
// carrying a different kind's prefix is rejected, not silently accepted —
// this is what stops a delivery ID from being usable where a payment ID is
// expected.
func decodeID(kind idKind, s string) (uuid.UUID, error) {
	rest, ok := strings.CutPrefix(s, kind.prefix)
	if !ok {
		return uuid.UUID{}, errInvalidID
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.UUID{}, errInvalidID
	}
	return id, nil
}
