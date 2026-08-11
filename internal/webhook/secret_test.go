package webhook_test

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"dummypay/internal/webhook"
)

func newTestKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

// TestEncryptDecrypt_RoundTrips verifies that encrypting then decrypting
// returns the exact original plaintext (ADR-0009).
func TestEncryptDecrypt_RoundTrips(t *testing.T) {
	key := newTestKey(t)
	plaintext := []byte("whsec_supersecretvalue1234567890")

	ciphertext, nonce, err := webhook.EncryptSecret(key, plaintext)
	require.NoError(t, err)

	decrypted, err := webhook.DecryptSecret(key, ciphertext, nonce)
	require.NoError(t, err)

	require.Equal(t, plaintext, decrypted)
}

// TestEncryptSecret_CiphertextDiffersAcrossEncryptions verifies that each
// encryption uses a fresh nonce, so two encryptions of the same plaintext
// produce different ciphertext (ADR-0009).
func TestEncryptSecret_CiphertextDiffersAcrossEncryptions(t *testing.T) {
	key := newTestKey(t)
	plaintext := []byte("whsec_supersecretvalue1234567890")

	ciphertext1, nonce1, err := webhook.EncryptSecret(key, plaintext)
	require.NoError(t, err)

	ciphertext2, nonce2, err := webhook.EncryptSecret(key, plaintext)
	require.NoError(t, err)

	require.NotEqual(t, ciphertext1, ciphertext2,
		"two encryptions of the same plaintext must produce different ciphertext")
	require.NotEqual(t, nonce1, nonce2,
		"two encryptions must use different nonces")
}

// TestDecryptSecret_TamperedCiphertext_FailsRatherThanReturningWrongBytes
// verifies that GCM authentication detects tampering rather than silently
// decrypting to garbage (ADR-0009).
func TestDecryptSecret_TamperedCiphertext_FailsRatherThanReturningWrongBytes(t *testing.T) {
	key := newTestKey(t)
	plaintext := []byte("whsec_supersecretvalue1234567890")

	ciphertext, nonce, err := webhook.EncryptSecret(key, plaintext)
	require.NoError(t, err)

	// Tamper with one byte of the ciphertext.
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[0] ^= 0xFF

	_, err = webhook.DecryptSecret(key, tampered, nonce)
	require.Error(t, err, "tampered ciphertext must fail to decrypt, not return wrong bytes")
}

// TestDecryptSecret_TamperedNonce_Fails verifies tampering with the nonce is
// also detected.
func TestDecryptSecret_TamperedNonce_Fails(t *testing.T) {
	key := newTestKey(t)
	plaintext := []byte("whsec_supersecretvalue1234567890")

	ciphertext, nonce, err := webhook.EncryptSecret(key, plaintext)
	require.NoError(t, err)

	tamperedNonce := make([]byte, len(nonce))
	copy(tamperedNonce, nonce)
	tamperedNonce[0] ^= 0xFF

	_, err = webhook.DecryptSecret(key, ciphertext, tamperedNonce)
	require.Error(t, err)
}

// TestEncryptSecret_WrongKeyLength_ReturnsError verifies the key must be
// exactly 32 bytes (AES-256).
func TestEncryptSecret_WrongKeyLength_ReturnsError(t *testing.T) {
	shortKey := make([]byte, 16)
	_, _, err := webhook.EncryptSecret(shortKey, []byte("data"))
	require.Error(t, err)
}
