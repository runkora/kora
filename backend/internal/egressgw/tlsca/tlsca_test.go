package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"
)

// newCA mints a fresh self-signed root CA for in-package tests. Mirrors
// testtlsca.NewCA but lives here to avoid an import cycle (testtlsca depends
// on this package, so this package's tests can't depend on testtlsca).
func newCA(t *testing.T, tenant string) (*IssuingCA, []byte) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Orbit Test CA — tenant=" + tenant,
			Organization: []string{"Orbit Egress Gateway (Test)"},
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          testSKID(&caKey.PublicKey),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	c, err := New(tenant, caCertPEM, caKeyPEM)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, caCertPEM
}

func testSKID(pub *ecdsa.PublicKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(der)
	out := make([]byte, 20)
	copy(out, sum[:20])
	return out
}

// exportTestKeyPEM returns the PKCS#8-encoded PEM for the CA's private key.
// Used by tests that exercise the New(...) parser path.
func exportTestKeyPEM(c *IssuingCA) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(c.caKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// certToPEM wraps a DER cert in a PEM block.
func certToPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestGetCertificate_HappyPath(t *testing.T) {
	ca, rootPEM := newCA(t, "acme")

	leaf, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "github.com"})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}

	if leaf.Leaf == nil {
		t.Fatal("leaf certificate is missing parsed Leaf")
	}
	if leaf.Leaf.Subject.CommonName != "github.com" {
		t.Errorf("CN = %q, want github.com", leaf.Leaf.Subject.CommonName)
	}
	if len(leaf.Leaf.DNSNames) != 1 || leaf.Leaf.DNSNames[0] != "github.com" {
		t.Errorf("DNSNames = %v, want [github.com]", leaf.Leaf.DNSNames)
	}

	// Tenant binding lives on the OU so audit can correlate cert → tenant.
	gotTenant := ""
	for _, ou := range leaf.Leaf.Subject.OrganizationalUnit {
		if rest, ok := strings.CutPrefix(ou, "tenant="); ok {
			gotTenant = rest
		}
	}
	if gotTenant != "acme" {
		t.Errorf("OU tenant = %q, want acme", gotTenant)
	}

	// The leaf must validate against the CA root we returned.
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(rootPEM); !ok {
		t.Fatal("failed to load root pem into pool")
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSName:   "github.com",
	}); err != nil {
		t.Errorf("leaf.Verify: %v", err)
	}
}

func TestGetCertificate_CachesPerHost(t *testing.T) {
	ca, _ := newCA(t, "acme")

	first, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "github.com"})
	if err != nil {
		t.Fatalf("first GetCertificate: %v", err)
	}
	second, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "github.com"})
	if err != nil {
		t.Fatalf("second GetCertificate: %v", err)
	}
	if first != second {
		t.Errorf("cache miss: same host should return identical *tls.Certificate pointer")
	}

	other, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "api.example.com"})
	if err != nil {
		t.Fatalf("third GetCertificate: %v", err)
	}
	if other == first {
		t.Errorf("different hosts must not share a cached leaf")
	}
}

func TestGetCertificate_RefreshesOnNearExpiry(t *testing.T) {
	ca, _ := newCA(t, "acme")

	base := time.Now()
	ca.now = func() time.Time { return base }

	first, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "github.com"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// Advance past the cache freshness margin so the cached leaf is treated as stale.
	ca.now = func() time.Time { return first.Leaf.NotAfter.Add(-leafCacheRefreshMargin / 2) }

	second, err := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "github.com"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first == second {
		t.Errorf("near-expiry leaf was not refreshed")
	}
}

func TestGetCertificate_RejectsEmptySNI(t *testing.T) {
	ca, _ := newCA(t, "acme")
	if _, err := ca.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
		t.Error("expected error on empty SNI")
	}
}

func TestNew_RoundTripFromPEM(t *testing.T) {
	src, rootPEM := newCA(t, "acme")
	keyPEM, err := exportTestKeyPEM(src)
	if err != nil {
		t.Fatalf("exportKey: %v", err)
	}

	rebuilt, err := New("acme", rootPEM, keyPEM)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rebuilt.Tenant() != "acme" {
		t.Errorf("Tenant() = %q, want acme", rebuilt.Tenant())
	}
	leaf, err := rebuilt.LeafFor("example.com")
	if err != nil {
		t.Fatalf("LeafFor: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(rebuilt.RootCertPEM())
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "example.com", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("leaf verify after PEM round-trip: %v", err)
	}
}

func TestNew_RejectsNonCACert(t *testing.T) {
	src, _ := newCA(t, "acme")
	leaf, err := src.LeafFor("example.com")
	if err != nil {
		t.Fatalf("LeafFor: %v", err)
	}
	leafCertPEM := certToPEM(leaf.Leaf.Raw)
	keyPEM, err := exportTestKeyPEM(src)
	if err != nil {
		t.Fatalf("exportKey: %v", err)
	}
	if _, err := New("acme", leafCertPEM, keyPEM); err == nil {
		t.Error("expected New to reject a non-CA cert")
	}
}

func TestNew_RejectsInvalidPEM(t *testing.T) {
	if _, err := New("acme", []byte("not pem"), []byte("not pem")); !errors.Is(err, ErrInvalidPEM) {
		t.Errorf("err = %v, want ErrInvalidPEM", err)
	}
}

func TestGetCertificate_ConcurrentMint(t *testing.T) {
	ca, _ := newCA(t, "acme")

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	errs := make(chan error, N)
	for range N {
		go func() {
			defer wg.Done()
			if _, e := ca.GetCertificate(&tls.ClientHelloInfo{ServerName: "github.com"}); e != nil {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent GetCertificate: %v", e)
	}
}
