package operator

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// testCA generates a throwaway self-signed CA for CSR-signing tests, reusing
// generateTenantCA so this file doesn't duplicate CA-minting logic.
func testCA(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	certPEM, keyPEM, err := generateTenantCA("csr-test")
	if err != nil {
		t.Fatalf("generate test CA: %v", err)
	}
	return certPEM, keyPEM
}

// testCSR generates a PEM-encoded CSR with the given common name, signed by
// a freshly generated ECDSA key the test discards — signKubeconfigCSR must
// never need or return that private key.
func testCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CSR key: %v", err)
	}
	template := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn, Organization: []string{"orbit:devenv"}},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func TestSignKubeconfigCSR_ValidCSRSignedWithCorrectExpiryAndScoping(t *testing.T) {
	caCertPEM, caKeyPEM := testCA(t)
	csrPEM := testCSR(t, "dev-alice")

	certPEM, err := signKubeconfigCSR(csrPEM, caCertPEM, caKeyPEM, kubeconfigCertValidity)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("result did not decode to a CERTIFICATE PEM block: %q", certPEM)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse signed cert: %v", err)
	}

	if cert.Subject.CommonName != "dev-alice" {
		t.Errorf("CN = %q, want %q (must carry the CSR's requested scoping through)", cert.Subject.CommonName, "dev-alice")
	}
	if cert.IsCA {
		t.Error("signed leaf certificate must not be a CA")
	}
	found := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			found = true
		}
	}
	if !found {
		t.Error("signed certificate missing ExtKeyUsageClientAuth")
	}

	gotValidity := cert.NotAfter.Sub(cert.NotBefore)
	if gotValidity < kubeconfigCertValidity || gotValidity > kubeconfigCertValidity+2*time.Minute {
		t.Errorf("validity = %v, want ~%v", gotValidity, kubeconfigCertValidity)
	}

	// Verify against the CA — the whole point of the exercise.
	caBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Errorf("signed certificate does not verify against the CA: %v", err)
	}
}

func TestSignKubeconfigCSR_MalformedCSRRejected(t *testing.T) {
	caCertPEM, caKeyPEM := testCA(t)

	cases := map[string][]byte{
		"not PEM at all":         []byte("this is not a PEM block"),
		"wrong PEM block type":   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")}),
		"CSR PEM type, junk DER": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte("junk")}),
		"empty":                  {},
	}
	for name, csr := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := signKubeconfigCSR(csr, caCertPEM, caKeyPEM, kubeconfigCertValidity); err == nil {
				t.Error("want error for malformed CSR, got nil")
			} else if !errors.Is(err, ErrInvalidCSR) {
				t.Errorf("want ErrInvalidCSR, got: %v", err)
			}
		})
	}
}

func TestSignKubeconfigCSR_TamperedSignatureRejected(t *testing.T) {
	caCertPEM, caKeyPEM := testCA(t)
	csrPEM := testCSR(t, "dev-bob")

	block, _ := pem.Decode(csrPEM)
	der := append([]byte(nil), block.Bytes...)
	// Flip a byte inside the DER body (well past the outer SEQUENCE header) to
	// invalidate the self-signature without producing unparseable ASN.1.
	der[len(der)-5] ^= 0xFF
	tampered := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})

	_, err := signKubeconfigCSR(tampered, caCertPEM, caKeyPEM, kubeconfigCertValidity)
	if err == nil {
		t.Fatal("want error for a CSR with an invalid signature, got nil")
	}
	if !errors.Is(err, ErrCSRSignatureInvalid) && !errors.Is(err, ErrInvalidCSR) {
		t.Errorf("want ErrCSRSignatureInvalid or ErrInvalidCSR (if tampering broke ASN.1 parsing instead), got: %v", err)
	}
}

func TestSignKubeconfigCSR_NoPrivateKeyMaterialOnResult(t *testing.T) {
	caCertPEM, caKeyPEM := testCA(t)
	csrPEM := testCSR(t, "dev-carol")

	certPEM, err := signKubeconfigCSR(csrPEM, caCertPEM, caKeyPEM, kubeconfigCertValidity)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// The only PEM block signKubeconfigCSR ever returns must be a public
	// certificate — asserting this directly is the "no private-key material
	// ever appears on the CR" property from the ADR's Ring 1 test plan,
	// since status.KubeconfigCertificate is populated verbatim from this value.
	block, rest := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("result did not decode as PEM")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("PEM block type = %q, want CERTIFICATE", block.Type)
	}
	if len(rest) != 0 {
		t.Errorf("unexpected trailing data after the certificate PEM block: %q", rest)
	}
}

func TestReconcileKubeconfigCSR_SignsAgainstVClusterCASecretAndPublishesStatus(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("csr-env", func(e *OrbitDevEnvironment) {
		e.Spec.KubeconfigCSR = string(testCSR(t, "dev-erin"))
	})
	r, _ := newDevEnvTestReconciler(t, clock, env)

	// A normal reconcile provisions the namespace the vcluster certs secret
	// would live in; create that secret directly rather than driving a real
	// vcluster install, matching this Ring 1 slice's "no live cluster" scope.
	reconcileOnce(t, r, env.Name)

	caCertPEM, caKeyPEM := testCA(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vclusterReleaseName + vclusterCertsSecretSuffix,
			Namespace: "orbit-devenv-csr-env",
		},
		Data: map[string][]byte{
			vclusterClientCACertKey: caCertPEM,
			vclusterClientCAKeyKey:  caKeyPEM,
		},
	}
	if err := r.Create(context.Background(), secret); err != nil {
		t.Fatalf("create fake vcluster certs secret: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.KubeconfigCertificate == "" {
		t.Fatal("status.kubeconfigCertificate was not populated")
	}
	if got.Status.KubeconfigCABundle != string(caCertPEM) {
		t.Error("status.kubeconfigCABundle does not match the vcluster's client CA cert")
	}

	block, _ := pem.Decode([]byte(got.Status.KubeconfigCertificate))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("status.kubeconfigCertificate is not a valid certificate PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse signed cert from status: %v", err)
	}
	if cert.Subject.CommonName != "dev-erin" {
		t.Errorf("signed cert CN = %q, want %q", cert.Subject.CommonName, "dev-erin")
	}

	// Re-reconciling must not re-sign (the CSR is fulfilled exactly once).
	firstCert := got.Status.KubeconfigCertificate
	reconcileOnce(t, r, env.Name)
	got = getEnv(t, r, env.Name)
	if got.Status.KubeconfigCertificate != firstCert {
		t.Error("kubeconfig CSR was re-signed on a subsequent reconcile")
	}
}

func TestReconcileKubeconfigCSR_ReSignsWhenSpecCSRChanges(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("csr-rotate-env", func(e *OrbitDevEnvironment) {
		e.Spec.KubeconfigCSR = string(testCSR(t, "dev-first"))
	})
	r, _ := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)
	caCertPEM, caKeyPEM := testCA(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vclusterReleaseName + vclusterCertsSecretSuffix,
			Namespace: "orbit-devenv-csr-rotate-env",
		},
		Data: map[string][]byte{
			vclusterClientCACertKey: caCertPEM,
			vclusterClientCAKeyKey:  caKeyPEM,
		},
	}
	if err := r.Create(context.Background(), secret); err != nil {
		t.Fatalf("create fake vcluster certs secret: %v", err)
	}
	reconcileOnce(t, r, env.Name)

	first := getEnv(t, r, env.Name)
	if first.Status.KubeconfigCertificate == "" {
		t.Fatal("first CSR was not signed")
	}
	firstCert := first.Status.KubeconfigCertificate

	// A second kubeconfig request (a fresh keypair bound to a new CSR) must
	// be signed too — the identity service cannot clear
	// status.KubeconfigCertificate itself (no status-subresource RBAC), so
	// the reconciler must key "already fulfilled" off the CSR's fingerprint,
	// not off KubeconfigCertificate merely being non-empty.
	live := getEnv(t, r, env.Name)
	live.Spec.KubeconfigCSR = string(testCSR(t, "dev-second"))
	if err := r.Update(context.Background(), live); err != nil {
		t.Fatalf("patch spec.kubeconfigCSR: %v", err)
	}
	reconcileOnce(t, r, env.Name)

	second := getEnv(t, r, env.Name)
	if second.Status.KubeconfigCertificate == firstCert {
		t.Fatal("second CSR was not re-signed — got back the first CSR's certificate")
	}
	block, _ := pem.Decode([]byte(second.Status.KubeconfigCertificate))
	if block == nil {
		t.Fatal("second status.kubeconfigCertificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse second signed cert: %v", err)
	}
	if cert.Subject.CommonName != "dev-second" {
		t.Errorf("second signed cert CN = %q, want %q", cert.Subject.CommonName, "dev-second")
	}
}

func TestReconcileKubeconfigCSR_NoCSRIsANoOp(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("no-csr-env", nil)
	r, _ := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.KubeconfigCertificate != "" {
		t.Error("kubeconfigCertificate should stay empty when spec.kubeconfigCSR is unset")
	}
	if got.Status.Phase != DevEnvPhaseReady {
		t.Errorf("phase = %q, want Ready (absence of a CSR must not block readiness)", got.Status.Phase)
	}
}

func TestSignKubeconfigCSR_DistinctSerialsPerCall(t *testing.T) {
	caCertPEM, caKeyPEM := testCA(t)
	csrPEM := testCSR(t, "dev-dana")

	a, err := signKubeconfigCSR(csrPEM, caCertPEM, caKeyPEM, kubeconfigCertValidity)
	if err != nil {
		t.Fatal(err)
	}
	b, err := signKubeconfigCSR(csrPEM, caCertPEM, caKeyPEM, kubeconfigCertValidity)
	if err != nil {
		t.Fatal(err)
	}

	ba, _ := pem.Decode(a)
	bb, _ := pem.Decode(b)
	ca, _ := x509.ParseCertificate(ba.Bytes)
	cb, _ := x509.ParseCertificate(bb.Bytes)
	if ca.SerialNumber.Cmp(cb.SerialNumber) == 0 {
		t.Error("two freshly-signed certs for the same CSR have the same serial number")
	}
}
