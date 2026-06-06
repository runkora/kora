package cabundle

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestLoadEager_HappyPath(t *testing.T) {
	root := t.TempDir()
	writeTenant(t, root, "acme")
	writeTenant(t, root, "globex")

	logger := logging.NewLoggerWithOutput(io.Discard)
	b, err := LoadEager(context.Background(), root, logger)
	if err != nil {
		t.Fatalf("LoadEager: %v", err)
	}

	for _, tenant := range []string{"acme", "globex"} {
		ca, err := b.For(tenant)
		if err != nil {
			t.Errorf("For(%q): %v", tenant, err)
			continue
		}
		if ca == nil {
			t.Errorf("For(%q) returned nil CA", tenant)
		}
	}

	if _, err := b.For("never-registered"); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("For(unknown): err = %v, want ErrTenantNotFound", err)
	}
}

func TestLoadEager_EmptyRoot(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	b, err := LoadEager(context.Background(), "", logger)
	if err != nil {
		t.Fatalf("LoadEager(\"\"): %v", err)
	}
	if _, err := b.For("acme"); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("empty bundle should reject lookups: err = %v", err)
	}
}

func TestLoadEager_MissingDirectory(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	b, err := LoadEager(context.Background(), "/no/such/path", logger)
	if err != nil {
		t.Fatalf("LoadEager on missing dir: want graceful start, got err: %v", err)
	}
	if _, err := b.For("acme"); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("expected empty bundle when root missing: err = %v", err)
	}
}

func TestLoadEager_MalformedTenantFails(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "broken")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "ca.crt"), []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "ca.key"), []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}

	logger := logging.NewLoggerWithOutput(io.Discard)
	if _, err := LoadEager(context.Background(), root, logger); err == nil {
		t.Error("expected LoadEager to fail on malformed tenant material")
	}
}

func TestLoadEager_IgnoresNonDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	// Stray file alongside tenant subdirs (e.g. a metadata file the operator wrote).
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTenant(t, root, "acme")

	logger := logging.NewLoggerWithOutput(io.Discard)
	b, err := LoadEager(context.Background(), root, logger)
	if err != nil {
		t.Fatalf("LoadEager: %v", err)
	}
	if _, err := b.For("acme"); err != nil {
		t.Errorf("acme: %v", err)
	}
}

// writeTenant generates a fresh self-signed CA and writes ca.crt + ca.key
// under {root}/{tenant}/ as the loader expects.
func writeTenant(t *testing.T, root, tenant string) {
	t.Helper()
	dir := filepath.Join(root, tenant)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "Test CA — tenant=" + tenant,
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}
