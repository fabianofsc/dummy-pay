package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// secretRandomBytes is the number of random bytes rendered into the
// generated subscription secret's base64url portion (spec §4.2).
const secretRandomBytes = 32

// GenerateSecret produces a new webhook subscription secret: whsec_ followed
// by secretRandomBytes of crypto/rand, base64url-encoded (spec §4.2).
func GenerateSecret() (string, error) {
	raw := make([]byte, secretRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

// keyLength is the required key size for AES-256. aes.NewCipher also accepts
// 16 and 24 bytes (AES-128/192), so this is checked explicitly rather than
// relying on the library to enforce the 256-bit requirement ADR-0009 commits to.
const keyLength = 32

// EncryptSecret encrypts plaintext with AES-256-GCM under key (must be
// exactly 32 bytes), using a freshly generated random nonce. Returns the
// ciphertext and the nonce, both of which must be stored to decrypt later
// (ADR-0009).
func EncryptSecret(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	if len(key) != keyLength {
		return nil, nil, fmt.Errorf("key must be %d bytes, got %d", keyLength, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// DecryptSecret reverses EncryptSecret. It fails rather than returning wrong
// bytes when ciphertext or nonce has been tampered with — GCM authenticates
// the ciphertext (ADR-0009).
func DecryptSecret(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}
