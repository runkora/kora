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
	// Both halves present but neither is valid PEM. The loader should error
	// rather than silently skip — malformed material is an operator config
	// problem we want to fail loud on.
	if err := os.WriteFile(filepath.Join(root, "broken.ca.crt"), []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.ca.key"), []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}

	logger := logging.NewLoggerWithOutput(io.Discard)
	if _, err := LoadEager(context.Background(), root, logger); err == nil {
		t.Error("expected LoadEager to fail on malformed tenant material")
	}
}

func TestLoadEager_IgnoresUnrelatedFilesAndStraySubdirs(t *testing.T) {
	root := t.TempDir()
	// Stray non-CA file at the root — kubelet sometimes drops these
	// (..data, ..2024_xx_xx symlinks); we must ignore them.
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Stray subdirectory — kubelet projects symlinks/directories that we
	// also need to skip without iterating into them.
	if err := os.MkdirAll(filepath.Join(root, "..data"), 0o755); err != nil {
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

func TestLoadEager_RequiresBothCertAndKey(t *testing.T) {
	root := t.TempDir()
	// Only the crt half — the key is missing. Must be skipped, not loaded
	// (and importantly, not raised as an error — partial material can
	// briefly appear during a kubelet projection update).
	if err := os.WriteFile(filepath.Join(root, "acme.ca.crt"), []byte("PEMBYTES"), 0o600); err != nil {
		t.Fatal(err)
	}

	logger := logging.NewLoggerWithOutput(io.Discard)
	b, err := LoadEager(context.Background(), root, logger)
	if err != nil {
		t.Fatalf("LoadEager: %v", err)
	}
	if _, err := b.For("acme"); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("expected acme to be skipped without ca.key; got %v", err)
	}
}

// writeTenant generates a fresh self-signed CA and writes
// {root}/{tenant}.ca.crt + {tenant}.ca.key — the same flat layout the
// operator's aggregated Secret produces when projected as a volume. Tests
// and production use the same on-disk shape so what we test is what runs.
func writeTenant(t *testing.T, root, tenant string) {
	t.Helper()

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
	if err := os.WriteFile(filepath.Join(root, tenant+".ca.crt"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(root, tenant+".ca.key"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}
