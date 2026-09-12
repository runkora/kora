package webhookcert

import (
	"crypto/x509"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestServingPair_SignedByCAWithServiceNames(t *testing.T) {
	ca, caKey, err := newCA()
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{Namespace: "orbit-system", ServiceName: "orbit-operator-webhook"}
	certPEM, _, err := m.newServingPair(ca, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := parseCert(ca)
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	for _, name := range []string{"orbit-operator-webhook", "orbit-operator-webhook.orbit-system.svc", "orbit-operator-webhook.orbit-system.svc.cluster.local"} {
		if _, err := cert.Verify(x509.VerifyOptions{DNSName: name, Roots: pool}); err != nil {
			t.Errorf("verify %s: %v", name, err)
		}
	}
	if cert.NotAfter.Sub(time.Now()) > servingValidity+time.Minute {
		t.Error("serving validity longer than configured")
	}
}

func TestCABundle_IncludesPreviousDuringRoll(t *testing.T) {
	sec := &corev1.Secret{Data: map[string][]byte{keyCACert: []byte("A"), keyCAPrevious: []byte("B")}}
	if got := string(caBundle(sec)); got != "AB" {
		t.Errorf("bundle = %q", got)
	}
	delete(sec.Data, keyCAPrevious)
	if got := string(caBundle(sec)); got != "A" {
		t.Errorf("bundle = %q", got)
	}
}
