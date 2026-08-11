package payment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
)

// fakeSecretGenerator returns a fixed value, so tests can assert on it.
type fakeSecretGenerator struct {
	secret string
	err    error
	calls  int
}

func (g *fakeSecretGenerator) NewSecret() (string, error) {
	g.calls++
	return g.secret, g.err
}

// fakeSubscriptionCreator records Create calls, and can be forced to fail —
// modelling the real partial unique index rejecting a second active
// subscription (proved for real, against a database, in Step 7.2).
type fakeSubscriptionCreator struct {
	err   error
	calls int

	gotID        uuid.UUID
	gotAccountID uuid.UUID
	gotURL       string
	gotEvents    []EventType
	gotSecret    string
	gotCreatedAt time.Time
}

func (c *fakeSubscriptionCreator) Create(_ context.Context, id, accountID uuid.UUID, url string, events []EventType, secretPlaintext string, createdAt time.Time) error {
	c.calls++
	c.gotID = id
	c.gotAccountID = accountID
	c.gotURL = url
	c.gotEvents = events
	c.gotSecret = secretPlaintext
	c.gotCreatedAt = createdAt
	return c.err
}

func newTestCreateSubscriptionUseCase(creator *fakeSubscriptionCreator, secrets *fakeSecretGenerator, now time.Time) *CreateSubscriptionUseCase {
	return NewCreateSubscriptionUseCase(CreateSubscriptionDeps{
		Subscriptions: creator,
		Secrets:       secrets,
		IDs:           clock.UUIDv7Generator{},
		Clock:         clock.NewFake(now),
	})
}

// TestCreateSubscriptionUseCase_Success_ReturnsPlaintextSecret verifies that
// a successful creation returns the plaintext secret exactly once, and
// passes it — not any derived form — to the repository for storage
// (spec §4.2, ADR-0009: encryption is the repository's job).
func TestCreateSubscriptionUseCase_Success_ReturnsPlaintextSecret(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	creator := &fakeSubscriptionCreator{}
	secrets := &fakeSecretGenerator{secret: "whsec_generated_value"}

	uc := newTestCreateSubscriptionUseCase(creator, secrets, now)

	accountID := uuid.New()
	events := []EventType{EventPaymentApproved, EventPaymentRejected}

	result, err := uc.Execute(context.Background(), CreateSubscriptionRequest{
		AccountID: accountID,
		URL:       "http://consumer.example/webhook",
		Events:    events,
	})
	require.NoError(t, err)

	require.Equal(t, "whsec_generated_value", result.Secret)
	require.Equal(t, "http://consumer.example/webhook", result.URL)
	require.Equal(t, events, result.Events)
	require.True(t, now.Equal(result.CreatedAt))
	require.NotEqual(t, uuid.Nil, result.SubscriptionID)

	require.Equal(t, 1, creator.calls)
	require.Equal(t, result.SubscriptionID, creator.gotID)
	require.Equal(t, accountID, creator.gotAccountID)
	require.Equal(t, "whsec_generated_value", creator.gotSecret)
	require.True(t, now.Equal(creator.gotCreatedAt))
}

// TestCreateSubscriptionUseCase_SecondActiveSubscription_PropagatesError
// verifies ErrSubscriptionExists from the repository is returned unchanged,
// so the HTTP layer can map it to 409 subscription_exists.
func TestCreateSubscriptionUseCase_SecondActiveSubscription_PropagatesError(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	creator := &fakeSubscriptionCreator{err: ErrSubscriptionExists}
	secrets := &fakeSecretGenerator{secret: "whsec_x"}

	uc := newTestCreateSubscriptionUseCase(creator, secrets, now)

	_, err := uc.Execute(context.Background(), CreateSubscriptionRequest{
		AccountID: uuid.New(),
		URL:       "http://consumer.example/webhook",
		Events:    []EventType{EventPaymentApproved},
	})
	require.ErrorIs(t, err, ErrSubscriptionExists)
}
