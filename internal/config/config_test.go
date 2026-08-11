package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dummypay/internal/config"
)

// validEnv returns a complete, valid environment. Each test mutates a copy.
func validEnv() map[string]string {
	return map[string]string{
		"DUMMYPAY_HTTP_ADDR":              ":9090",
		"DUMMYPAY_DATABASE_URL":           "postgres://dummypay:dummypay@localhost:5432/dummypay?sslmode=disable",
		"DUMMYPAY_ACCOUNT_KEY_ID":         "acct_test",
		"DUMMYPAY_ACCOUNT_KEY_SECRET":     "a-secret-of-16-chars-or-more",
		"DUMMYPAY_WEBHOOK_SECRET_ENC_KEY": "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=", // 32 bytes, base64
		"DUMMYPAY_PROCESSING_DELAY":       "1s",
		"DUMMYPAY_IDEMPOTENCY_LEASE":      "10s",
		"DUMMYPAY_WORKER_POLL_INTERVAL":   "100ms",
		"DUMMYPAY_WEBHOOK_TIMEOUT":        "2s",
	}
}

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestLoad_AllValid_ProducesExpectedConfig(t *testing.T) {
	cfg, err := config.Load(lookupFrom(validEnv()))
	require.NoError(t, err)

	require.Equal(t, ":9090", cfg.HTTPAddr)
	require.Equal(t, "postgres://dummypay:dummypay@localhost:5432/dummypay?sslmode=disable", cfg.DatabaseURL)
	require.Equal(t, "acct_test", cfg.AccountKeyID)
	require.Equal(t, "a-secret-of-16-chars-or-more", cfg.AccountKeySecret)
	require.Len(t, cfg.WebhookSecretEncKey, 32)
	require.Equal(t, time.Second, cfg.ProcessingDelay)
	require.Equal(t, 10*time.Second, cfg.IdempotencyLease)
	require.Equal(t, 100*time.Millisecond, cfg.WorkerPollInterval)
	require.Equal(t, 2*time.Second, cfg.WebhookTimeout)
}

func TestLoad_Defaults_ApplyWhenOptionalsAbsent(t *testing.T) {
	env := validEnv()
	delete(env, "DUMMYPAY_HTTP_ADDR")
	delete(env, "DUMMYPAY_PROCESSING_DELAY")
	delete(env, "DUMMYPAY_IDEMPOTENCY_LEASE")
	delete(env, "DUMMYPAY_WORKER_POLL_INTERVAL")
	delete(env, "DUMMYPAY_WEBHOOK_TIMEOUT")

	cfg, err := config.Load(lookupFrom(env))
	require.NoError(t, err)

	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, 3*time.Second, cfg.ProcessingDelay)
	require.Equal(t, 30*time.Second, cfg.IdempotencyLease)
	require.Equal(t, 250*time.Millisecond, cfg.WorkerPollInterval)
	require.Equal(t, 5*time.Second, cfg.WebhookTimeout)
}

func TestLoad_RequiredVariableMissing_NamesTheVariable(t *testing.T) {
	required := []string{
		"DUMMYPAY_DATABASE_URL",
		"DUMMYPAY_ACCOUNT_KEY_ID",
		"DUMMYPAY_ACCOUNT_KEY_SECRET",
		"DUMMYPAY_WEBHOOK_SECRET_ENC_KEY",
	}

	for _, variable := range required {
		t.Run(variable, func(t *testing.T) {
			env := validEnv()
			delete(env, variable)

			_, err := config.Load(lookupFrom(env))

			require.Error(t, err)
			require.ErrorContains(t, err, variable)
		})
	}
}

func TestLoad_MalformedVariable_NamesTheVariable(t *testing.T) {
	cases := []struct {
		name     string
		variable string
		value    string
	}{
		{"empty account key id", "DUMMYPAY_ACCOUNT_KEY_ID", ""},
		{"account key secret too short", "DUMMYPAY_ACCOUNT_KEY_SECRET", "short"},
		{"database url not a URL", "DUMMYPAY_DATABASE_URL", "not-a-url"},
		{"database url wrong scheme", "DUMMYPAY_DATABASE_URL", "mysql://localhost/db"},
		{"http addr unparseable", "DUMMYPAY_HTTP_ADDR", "not-an-address"},
		{"webhook key not base64", "DUMMYPAY_WEBHOOK_SECRET_ENC_KEY", "not-valid-base64!!!"},
		{"webhook key wrong length", "DUMMYPAY_WEBHOOK_SECRET_ENC_KEY", "c2hvcnQ="}, // "short", 5 bytes
		{"processing delay not a duration", "DUMMYPAY_PROCESSING_DELAY", "soon"},
		{"processing delay negative", "DUMMYPAY_PROCESSING_DELAY", "-1s"},
		{"idempotency lease not a duration", "DUMMYPAY_IDEMPOTENCY_LEASE", "a while"},
		{"idempotency lease zero", "DUMMYPAY_IDEMPOTENCY_LEASE", "0s"},
		{"idempotency lease negative", "DUMMYPAY_IDEMPOTENCY_LEASE", "-5s"},
		{"worker poll interval zero", "DUMMYPAY_WORKER_POLL_INTERVAL", "0s"},
		{"webhook timeout zero", "DUMMYPAY_WEBHOOK_TIMEOUT", "0s"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			env[tc.variable] = tc.value

			_, err := config.Load(lookupFrom(env))

			require.Error(t, err)
			require.ErrorContains(t, err, tc.variable)
		})
	}
}

// The one case the plan calls out explicitly, kept as its own named test
// rather than only a row in the table above.
func TestLoad_WebhookSecretEncKey_MustBe32BytesAfterDecoding(t *testing.T) {
	env := validEnv()
	env["DUMMYPAY_WEBHOOK_SECRET_ENC_KEY"] = "dG9vLXNob3J0" // "too-short", 9 bytes

	_, err := config.Load(lookupFrom(env))

	require.Error(t, err)
	require.ErrorContains(t, err, "DUMMYPAY_WEBHOOK_SECRET_ENC_KEY")
}

// PROCESSING_DELAY explicitly allows zero ("0s in tests", README); the other
// three durations require a strictly positive value. Same validator applied
// to all four would hide this distinction, so it gets its own test.
func TestLoad_ProcessingDelay_ZeroIsValid(t *testing.T) {
	env := validEnv()
	env["DUMMYPAY_PROCESSING_DELAY"] = "0s"

	cfg, err := config.Load(lookupFrom(env))

	require.NoError(t, err)
	require.Equal(t, time.Duration(0), cfg.ProcessingDelay)
}
