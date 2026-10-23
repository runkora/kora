package triggers

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestSecretCipher_RoundTrip(t *testing.T) {
	c, err := newSecretCipher(testCipherKey)
	if err != nil {
		t.Fatalf("newSecretCipher: %v", err)
	}
	plaintext := "super-secret-webhook-signing-key"

	ct, err := c.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ct == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := c.decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestSecretCipher_NonceIsRandom(t *testing.T) {
	c, _ := newSecretCipher(testCipherKey)
	a, _ := c.encrypt("same")
	b, _ := c.encrypt("same")
	if a == b {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestNewSecretCipher_BadKeySize(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		if _, err := newSecretCipher(make([]byte, n)); !errors.Is(err, ErrCipherKey) {
			t.Errorf("key size %d: expected ErrCipherKey, got %v", n, err)
		}
	}
}

func TestSecretCipher_DecryptErrors(t *testing.T) {
	c, _ := newSecretCipher(testCipherKey)

	if _, err := c.decrypt("!!!not-base64!!!"); err == nil {
		t.Error("expected error decoding invalid base64")
	}
	if _, err := c.decrypt(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("expected error on too-short ciphertext")
	}

	// Ciphertext from a different key must fail authentication.
	other, _ := newSecretCipher([]byte("ffffffffffffffffffffffffffffffff"))
	ct, _ := other.encrypt("secret")
	if _, err := c.decrypt(ct); err == nil {
		t.Error("expected decrypt failure with the wrong key")
	}
}

func TestGenerateSecret(t *testing.T) {
	a, err := generateSecret()
	if err != nil {
		t.Fatalf("generateSecret: %v", err)
	}
	b, _ := generateSecret()
	if a == "" || b == "" {
		t.Fatal("generated secret must be non-empty")
	}
	if a == b {
		t.Fatal("generated secrets must be unique")
	}
	// RawURLEncoding output must not contain padding or URL-unsafe characters.
	if strings.ContainsAny(a, "+/=") {
		t.Errorf("secret should be URL-safe without padding: %q", a)
	}
}

// An unset key refuses to start. There is deliberately no fallback: the key
// protects every tenant's webhook signing secrets, so booting without one would
// mean encrypting them under whatever constant the fallback used.
func TestLoadCipherKey_UnsetIsFatal(t *testing.T) {
	t.Setenv("KORA_TRIGGERS_ENCRYPTION_KEY", "")
	key, err := LoadCipherKey()
	if !errors.Is(err, ErrCipherKey) {
		t.Fatalf("expected ErrCipherKey when env unset, got err=%v", err)
	}
	if key != nil {
		t.Error("must not return a key when the env var is unset")
	}
}

func TestLoadCipherKey_FromEnv(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	t.Setenv("KORA_TRIGGERS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(raw))
	key, err := LoadCipherKey()
	if err != nil {
		t.Fatalf("unexpected error for valid env key: %v", err)
	}
	if string(key) != string(raw) {
		t.Error("loaded key must match the env-provided key")
	}
}

func TestLoadCipherKey_SetButInvalidIsFatal(t *testing.T) {
	// A present-but-malformed key must be a hard error. Cover both failure
	// modes: bad base64 and wrong length.
	cases := map[string]string{
		"not base64":   "@@@not-base64@@@",
		"wrong length": base64.StdEncoding.EncodeToString([]byte("too-short")),
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("KORA_TRIGGERS_ENCRYPTION_KEY", val)
			key, err := LoadCipherKey()
			if !errors.Is(err, ErrCipherKey) {
				t.Fatalf("expected ErrCipherKey for a set-but-invalid key, got err=%v", err)
			}
			if key != nil {
				t.Error("must not return a key for a set-but-invalid key")
			}
		})
	}
}
