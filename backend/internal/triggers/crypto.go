package triggers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// secretCipher encrypts webhook signing secrets at rest with AES-256-GCM.
//
// The PRD calls for signing secrets never to be stored in plaintext. This is the
// pragmatic v1 mechanism: the secret is encrypted with a platform-held key
// (loaded from the environment) and persisted as ciphertext. The key never lives
// in YAML or the database.
type secretCipher struct {
	gcm cipher.AEAD
}

// ErrCipherKey indicates the encryption key is missing or the wrong size.
var ErrCipherKey = errors.New("invalid triggers encryption key")

// newSecretCipher builds a secretCipher from a 32-byte key.
func newSecretCipher(key []byte) (*secretCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: key must be 32 bytes, got %d", ErrCipherKey, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &secretCipher{gcm: gcm}, nil
}

// encrypt returns base64(nonce || ciphertext) for the given plaintext.
func (c *secretCipher) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decrypt reverses encrypt.
func (c *secretCipher) decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	ns := c.gcm.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("%w: ciphertext too short", ErrCipherKey)
	}
	nonce, ct := raw[:ns], raw[ns:]
	plaintext, err := c.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// LoadCipherKey resolves the AES-256 key for encrypting webhook signing secrets.
//
// It reads a base64-encoded 32-byte key from ORBIT_TRIGGERS_ENCRYPTION_KEY. When
// the variable is unset (local development), it derives a stable well-known key
// from a fixed passphrase so the feature works out of the box — devFallback is
// true so the caller can log a warning. Production deployments MUST set the
// variable.
//
// A variable that is set but malformed (not valid base64, or not 32 bytes) is a
// fatal misconfiguration: it returns ErrCipherKey rather than silently falling
// back to the public dev key, which would leave every signing secret encrypted
// under a well-known key.
func LoadCipherKey() (key []byte, devFallback bool, err error) {
	if raw := os.Getenv("ORBIT_TRIGGERS_ENCRYPTION_KEY"); raw != "" {
		decoded, decErr := base64.StdEncoding.DecodeString(raw)
		if decErr != nil {
			return nil, false, fmt.Errorf("%w: ORBIT_TRIGGERS_ENCRYPTION_KEY is not valid base64: %w", ErrCipherKey, decErr)
		}
		if len(decoded) != 32 {
			return nil, false, fmt.Errorf("%w: ORBIT_TRIGGERS_ENCRYPTION_KEY must decode to 32 bytes, got %d", ErrCipherKey, len(decoded))
		}
		return decoded, false, nil
	}
	sum := sha256.Sum256([]byte("orbit-triggers-local-dev-key"))
	return sum[:], true, nil
}

// generateSecret returns a cryptographically random hex string suitable for use
// as a webhook HMAC signing secret.
func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
