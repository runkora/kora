// Kubeconfig CSR fulfillment for OrbitDevEnvironment (decision 6 of the
// prototype spike).
//
// The identity service (Phase 3, not yet built) generates a client keypair on
// the developer's behalf, places the CSR on the CR, and never holds the
// vcluster's CA key itself — the operator (this file) reads the CA directly
// from the vcluster's own Secret and signs. Only public material — the CSR
// and the resulting certificate — ever crosses the CR boundary; the vcluster
// CA's private key is read here and discarded, never written anywhere.
package operator

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/clear-route/orbit/internal/platform/orbitapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// kubeconfigCertValidity bounds how long a signed kubeconfig client
// certificate is valid for (decision 6's "default 8h").
const kubeconfigCertValidity = 8 * time.Hour

// vclusterCertsSecretSuffix + vclusterReleaseName gives the Secret the
// vcluster chart creates holding its internal PKI, including the client CA
// the apiserver validates client certificates against (confirmed against a
// live vcluster's apiserver args: --client-ca-file=/data/pki/client-ca.crt).
const vclusterCertsSecretSuffix = "-certs"

const (
	vclusterClientCACertKey = "client-ca.crt"
	vclusterClientCAKeyKey  = "client-ca.key"
)

var (
	// ErrInvalidCSR is returned when the supplied CSR is not a well-formed,
	// self-consistent PKCS#10 request.
	ErrInvalidCSR = errors.New("invalid certificate signing request")
	// ErrCSRSignatureInvalid is returned when the CSR's self-signature does
	// not verify against its own embedded public key.
	ErrCSRSignatureInvalid = errors.New("certificate signing request signature does not verify")
)

// reconcileKubeconfigCSR fulfills spec.KubeconfigCSR, if present and not
// already fulfilled, by signing it against the vcluster's own client CA.
// "Already fulfilled" is judged by comparing orbitapi.CSRFingerprint of the
// current spec.KubeconfigCSR against status.KubeconfigCSRFingerprint, not by
// whether status.KubeconfigCertificate is merely non-empty — the caller
// (identity service) requests a fresh kubeconfig by patching spec with a new
// CSR bound to a new keypair, and its RBAC has no access to the status
// subresource to clear KubeconfigCertificate itself. Comparing fingerprints
// re-signs exactly when the CSR actually changed, and stays a no-op on every
// other reconcile in between.
func (r *DevEnvironmentReconciler) reconcileKubeconfigCSR(ctx context.Context, env *OrbitDevEnvironment) error {
	if env.Spec.KubeconfigCSR == "" {
		return nil
	}
	if fp := orbitapi.CSRFingerprint([]byte(env.Spec.KubeconfigCSR)); fp == env.Status.KubeconfigCSRFingerprint {
		return nil
	}

	ns := devEnvNamespace(env)
	secretName := vclusterReleaseName + vclusterCertsSecretSuffix

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: ns}, secret); err != nil {
		return fmt.Errorf("get vcluster CA secret %q: %w", secretName, err)
	}
	caCertPEM, ok := secret.Data[vclusterClientCACertKey]
	if !ok {
		return fmt.Errorf("vcluster CA secret %q missing %q", secretName, vclusterClientCACertKey)
	}
	caKeyPEM, ok := secret.Data[vclusterClientCAKeyKey]
	if !ok {
		return fmt.Errorf("vcluster CA secret %q missing %q", secretName, vclusterClientCAKeyKey)
	}

	certPEM, err := signKubeconfigCSR([]byte(env.Spec.KubeconfigCSR), caCertPEM, caKeyPEM, kubeconfigCertValidity)
	if err != nil {
		_ = r.setDevEnvCondition(ctx, env, ConditionDevEnvKubeconfigCSR, metav1.ConditionFalse,
			"SignFailed", err.Error())
		return fmt.Errorf("sign kubeconfig CSR for %q: %w", env.Name, err)
	}

	if err := r.patchStatusFields(ctx, env, func(s *OrbitDevEnvironmentStatus) {
		s.KubeconfigCertificate = string(certPEM)
		s.KubeconfigCABundle = string(caCertPEM)
		s.KubeconfigCSRFingerprint = orbitapi.CSRFingerprint([]byte(env.Spec.KubeconfigCSR))
		// Minting a kubeconfig is a real platform-visible activity signal
		// (decision 8) available today, independent of the identity service
		// (Phase 3) that will eventually originate these requests.
		t := metav1.NewTime(r.Clock.Now())
		s.LastActivityAt = &t
	}); err != nil {
		return err
	}
	return r.setDevEnvCondition(ctx, env, ConditionDevEnvKubeconfigCSR, metav1.ConditionTrue,
		"Signed", "kubeconfig CSR signed against the vcluster's client CA")
}

// signKubeconfigCSR validates and signs a PEM-encoded PKCS#10 CSR against the
// given CA certificate/key, returning a PEM-encoded client certificate.
// caKeyPEM is read by the caller from a live Secret and never persisted —
// this function holds it only for the duration of the signing operation and
// never returns or logs it.
func signKubeconfigCSR(csrPEM, caCertPEM, caKeyPEM []byte, validity time.Duration) ([]byte, error) {
	csr, err := parseAndVerifyCSR(csrPEM)
	if err != nil {
		return nil, err
	}

	caCert, caKey, err := parseCA(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse vcluster CA: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    now.Add(-1 * time.Minute),
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, csr.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func parseAndVerifyCSR(csrPEM []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("%w: not a PEM CERTIFICATE REQUEST block", ErrInvalidCSR)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCSR, err) //nolint:errorlint // wrapping a sentinel plus a dynamic detail, not the error itself
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCSRSignatureInvalid, err) //nolint:errorlint
	}
	return csr, nil
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, crypto.Signer, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, errors.New("CA certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, errors.New("CA key is not valid PEM")
	}
	key, err := parseCASigner(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}
	return cert, key, nil
}

// parseCASigner tries the private key encodings a Kubernetes-style PKI
// directory (kubeadm, and vcluster's k8s distro which mirrors it) may use.
func parseCASigner(der []byte) (crypto.Signer, error) {
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		signer, ok := k.(crypto.Signer)
		if !ok {
			return nil, errors.New("PKCS8 key does not implement crypto.Signer")
		}
		return signer, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(der); err == nil {
		return k, nil
	}
	return nil, errors.New("unsupported private key encoding")
}
