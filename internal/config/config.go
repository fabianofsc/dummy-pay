// Package config parses and validates configuration from environment
// variables only; no configuration file is ever read (ADR-0010). See
// docs/spec-v1.md §9.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"time"
)

// Config is the immutable, fully validated configuration for the service.
type Config struct {
	HTTPAddr            string
	DatabaseURL         string
	AccountKeyID        string
	AccountKeySecret    string
	WebhookSecretEncKey []byte
	ProcessingDelay     time.Duration
	IdempotencyLease    time.Duration
	WorkerPollInterval  time.Duration
	WebhookTimeout      time.Duration
}

// Lookup mirrors os.LookupEnv's signature so tests can supply a fake
// environment without mutating process state.
type Lookup func(key string) (value string, ok bool)

const (
	defaultHTTPAddr           = ":8080"
	defaultProcessingDelay    = 3 * time.Second
	defaultIdempotencyLease   = 30 * time.Second
	defaultWorkerPollInterval = 250 * time.Millisecond
	defaultWebhookTimeout     = 5 * time.Second
)

// Load reads and validates every variable from spec §9 via lookup, and
// returns the first validation failure encountered, naming the variable it
// came from. It never reads a file.
func Load(lookup Lookup) (Config, error) {
	var cfg Config
	var err error

	if cfg.HTTPAddr, err = optionalListenAddr(lookup, "DUMMYPAY_HTTP_ADDR", defaultHTTPAddr); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseURL, err = requiredPostgresDSN(lookup, "DUMMYPAY_DATABASE_URL"); err != nil {
		return Config{}, err
	}
	if cfg.AccountKeyID, err = requiredNonEmpty(lookup, "DUMMYPAY_ACCOUNT_KEY_ID"); err != nil {
		return Config{}, err
	}
	if cfg.AccountKeySecret, err = requiredMinLength(lookup, "DUMMYPAY_ACCOUNT_KEY_SECRET", 16); err != nil {
		return Config{}, err
	}
	if cfg.WebhookSecretEncKey, err = requiredBase64Bytes(lookup, "DUMMYPAY_WEBHOOK_SECRET_ENC_KEY", 32); err != nil {
		return Config{}, err
	}
	if cfg.ProcessingDelay, err = optionalDuration(lookup, "DUMMYPAY_PROCESSING_DELAY", defaultProcessingDelay, true); err != nil {
		return Config{}, err
	}
	if cfg.IdempotencyLease, err = optionalDuration(lookup, "DUMMYPAY_IDEMPOTENCY_LEASE", defaultIdempotencyLease, false); err != nil {
		return Config{}, err
	}
	if cfg.WorkerPollInterval, err = optionalDuration(lookup, "DUMMYPAY_WORKER_POLL_INTERVAL", defaultWorkerPollInterval, false); err != nil {
		return Config{}, err
	}
	if cfg.WebhookTimeout, err = optionalDuration(lookup, "DUMMYPAY_WEBHOOK_TIMEOUT", defaultWebhookTimeout, false); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func fail(variable, reason string) error {
	return fmt.Errorf("%s: %s", variable, reason)
}

func requiredNonEmpty(lookup Lookup, variable string) (string, error) {
	v, ok := lookup(variable)
	if !ok || v == "" {
		return "", fail(variable, "required and must not be empty")
	}
	return v, nil
}

func requiredMinLength(lookup Lookup, variable string, min int) (string, error) {
	v, err := requiredNonEmpty(lookup, variable)
	if err != nil {
		return "", err
	}
	if len(v) < min {
		return "", fail(variable, fmt.Sprintf("must be at least %d characters", min))
	}
	return v, nil
}

func requiredPostgresDSN(lookup Lookup, variable string) (string, error) {
	v, err := requiredNonEmpty(lookup, variable)
	if err != nil {
		return "", err
	}
	u, parseErr := url.Parse(v)
	if parseErr != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fail(variable, "must be a valid postgres:// DSN")
	}
	return v, nil
}

func requiredBase64Bytes(lookup Lookup, variable string, length int) ([]byte, error) {
	v, err := requiredNonEmpty(lookup, variable)
	if err != nil {
		return nil, err
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(v)
	if decodeErr != nil {
		return nil, fail(variable, "must be base64-encoded")
	}
	if len(decoded) != length {
		return nil, fail(variable, fmt.Sprintf("must decode to exactly %d bytes, got %d", length, len(decoded)))
	}
	return decoded, nil
}

func optionalListenAddr(lookup Lookup, variable, def string) (string, error) {
	v, ok := lookup(variable)
	if !ok || v == "" {
		v = def
	}
	if _, _, err := net.SplitHostPort(v); err != nil {
		return "", fail(variable, "must be a valid listen address (host:port)")
	}
	return v, nil
}

// optionalDuration parses a Go duration, applying def when the variable is
// absent. allowZero distinguishes PROCESSING_DELAY ("not negative", zero is
// valid — README documents 0s in tests) from the other durations
// ("positive", zero is rejected).
func optionalDuration(lookup Lookup, variable string, def time.Duration, allowZero bool) (time.Duration, error) {
	v, ok := lookup(variable)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fail(variable, "must be a valid Go duration")
	}
	if d < 0 || (d == 0 && !allowZero) {
		if allowZero {
			return 0, fail(variable, "must not be negative")
		}
		return 0, fail(variable, "must be positive")
	}
	return d, nil
}
