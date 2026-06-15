package operator

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestGenerateTenantCA_ProducesValidPEM(t *testing.T) {
	certPEM, keyPEM, err := generateTenantCA("acme")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Cert PEM must decode to an x509 certificate that is a CA, signed by itself.
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("cert PEM did not decode: %q", certPEM)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if !cert.IsCA {
		t.Error("cert is not a CA")
	}
	if !strings.Contains(cert.Subject.CommonName, "acme") {
		t.Errorf("CN missing tenant marker: %q", cert.Subject.CommonName)
	}
	// Self-signed: issuer == subject.
	if cert.Issuer.CommonName != cert.Subject.CommonName {
		t.Errorf("not self-signed: issuer=%q subject=%q", cert.Issuer.CommonName, cert.Subject.CommonName)
	}
	// Validity ~ 10 years, give a generous tolerance for clock skew.
	gotValidity := cert.NotAfter.Sub(cert.NotBefore)
	if gotValidity < caCertValidity-time.Hour || gotValidity > caCertValidity+time.Hour {
		t.Errorf("validity = %v, want ~%v", gotValidity, caCertValidity)
	}

	// Key PEM must decode to a usable PKCS#8 private key.
	block, _ = pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("key PEM did not decode: %q", keyPEM)
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		t.Fatalf("parse key: %v", err)
	}
}

func TestGenerateTenantCA_DistinctSerialsPerCall(t *testing.T) {
	a, _, err := generateTenantCA("t1")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := generateTenantCA("t1")
	if err != nil {
		t.Fatal(err)
	}
	// Two CAs minted back-to-back must not collide on serial number — that
	// would mean we're somehow not using rand.Int correctly.
	pa, _ := pem.Decode(a)
	pb, _ := pem.Decode(b)
	ca, _ := x509.ParseCertificate(pa.Bytes)
	cb, _ := x509.ParseCertificate(pb.Bytes)
	if ca.SerialNumber.Cmp(cb.SerialNumber) == 0 {
		t.Error("two freshly-minted CAs have the same serial number")
	}
}
