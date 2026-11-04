// Package testtlsca provides test fixtures for the egress gateway's TLS
// authority. It exists as its own package (instead of a tlsca _test.go file)
// because consumers in other packages need to construct test CAs for their own
// integration tests: egressgw's CONNECT tests being the primary use.
//
// Production code MUST NOT import this package. Doing so would compile a
// self-signed root CA generator into the binary, which is a real risk if a
// future binary's main happened to call it. The package's documented purpose
// makes the import a clear smell during review; CI may add a depguard rule
// banning testtlsca from non-_test.go files in the future.
package testtlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/runkora/kora/backend/internal/egressgw/tlsca"
)

// NewCA mints a fresh self-signed root CA suitable for tests. Returns an
// IssuingCA wrapping the root, plus the root cert PEM tests should load into
// a CertPool so the test client trusts the chain.
func NewCA(tenant string) (*tlsca.IssuingCA, []byte, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "kora Test CA: tenant=" + tenant,
			Organization: []string{"kora Egress Gateway (Test)"},
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          skid(&caKey.PublicKey),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		return nil, nil, err
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	c, err := tlsca.New(tenant, caCertPEM, caKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	return c, caCertPEM, nil
}

// randomSerial returns a 128-bit positive serial number.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	for {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, err
		}
		if n.Sign() > 0 {
			return n, nil
		}
	}
}

// skid computes a Subject Key Identifier for the test CA. SHA-256 truncated to
// 20 bytes.
func skid(pub *ecdsa.PublicKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(der)
	out := make([]byte, 20)
	copy(out, sum[:20])
	return out
}
