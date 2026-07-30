package triggers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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

// LoadCipherKey resolves the AES-256 key for encrypting webhook signing secrets
// from ORBIT_TRIGGERS_ENCRYPTION_KEY, base64-encoded and 32 bytes.
//
// Required, with no fallback. There used to be one: an unset variable derived a
// stable key from a fixed passphrase so the feature worked out of the box, and
// the caller logged a warning. That is the wrong shape for this. The key
// protects every tenant's webhook signing secrets, the fallback was a constant
// in this file, and "starts anyway with a warning" meant a production deployment
// that forgot the variable encrypted every secret under a key published in the
// repository and stayed up to keep doing it. A missing key is now a refusal to
// start, so the failure is at boot and unmissable rather than silent and
// permanent. Environments supply the key themselves: `make setup` generates one
// into .env for local, and create-secrets.sh puts it in orbit-secrets.
func LoadCipherKey() ([]byte, error) {
	raw := os.Getenv("ORBIT_TRIGGERS_ENCRYPTION_KEY")
	if raw == "" {
		return nil, fmt.Errorf("%w: ORBIT_TRIGGERS_ENCRYPTION_KEY is required (base64-encoded 32 bytes)", ErrCipherKey)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: ORBIT_TRIGGERS_ENCRYPTION_KEY is not valid base64: %w", ErrCipherKey, err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("%w: ORBIT_TRIGGERS_ENCRYPTION_KEY must decode to 32 bytes, got %d", ErrCipherKey, len(decoded))
	}
	return decoded, nil
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
