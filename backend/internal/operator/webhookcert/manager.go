// Package webhookcert manages serving certificates for kora admission webhooks.
package webhookcert

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const (
	// SecretName holds the CA and serving key pair in the operator's namespace.
	SecretName = "orbit-operator-webhook-tls"

	keyCACert       = "ca.crt"
	keyCAKey        = "ca.key"
	keyCAPrevious   = "ca-previous.crt"
	keyTLSCert      = "tls.crt"
	keyTLSKey       = "tls.key"
	caValidity      = 5 * 365 * 24 * time.Hour
	servingValidity = 365 * 24 * time.Hour
	renewBefore     = 30 * 24 * time.Hour
)

// ErrNotReady is returned while the serving certificate is not yet written.
var ErrNotReady = errors.New("webhook certificate not ready")

// Webhooks are the configurations the manager keeps wired.
var Webhooks = struct {
	Mutating   string
	Validating string
}{
	Mutating:   "aaa-orbit-agent-pod-mutator",
	Validating: "orbit-agent-pod-validator",
}

// Manager owns the webhook certificate lifecycle for one operator process.
type Manager struct {
	Client      client.Client
	Logger      logging.Logger
	Namespace   string
	ServiceName string
	CertDir     string

	// External is true when cert-manager issues the certificate; the manager
	// then only patches failurePolicy and leaves caBundle to the cainjector.
	External bool
}

// Bootstrap ensures the Secret exists (creating it or adopting a replica's),
// writes the serving pair to CertDir so the webhook server can start, and
// returns the CA bundle to publish.
func (m *Manager) Bootstrap(ctx context.Context) error {
	if m.External {
		return nil
	}
	sec, err := m.ensureSecret(ctx)
	if err != nil {
		return err
	}
	return m.writeCertDir(sec)
}

// Reconcile refreshes local certificate files and webhook configuration.
func (m *Manager) Reconcile(ctx context.Context) error {
	var bundle []byte
	if !m.External {
		sec := &corev1.Secret{}
		if err := m.Client.Get(ctx, client.ObjectKey{Name: SecretName, Namespace: m.Namespace}, sec); err != nil {
			return fmt.Errorf("get webhook secret: %w", err)
		}
		if err := m.writeCertDir(sec); err != nil {
			return err
		}
		bundle = caBundle(sec)
	}
	for _, name := range []string{Webhooks.Mutating, Webhooks.Validating} {
		if err := m.patchWebhook(ctx, name, bundle); err != nil {
			return err
		}
	}
	return nil
}

// Rotate renews certificates before expiry. Only the leader rotates shared state.
func (m *Manager) Rotate(ctx context.Context) error {
	if m.External {
		return nil
	}
	sec := &corev1.Secret{}
	if err := m.Client.Get(ctx, client.ObjectKey{Name: SecretName, Namespace: m.Namespace}, sec); err != nil {
		return fmt.Errorf("get webhook secret: %w", err)
	}
	caCert, err := parseCert(sec.Data[keyCACert])
	if err != nil {
		return fmt.Errorf("parse ca.crt: %w", err)
	}
	serving, err := parseCert(sec.Data[keyTLSCert])
	if err != nil {
		return fmt.Errorf("parse tls.crt: %w", err)
	}
	now := time.Now()
	rollCA := caCert.NotAfter.Sub(now) < renewBefore
	renewServing := rollCA || serving.NotAfter.Sub(now) < renewBefore
	if !renewServing {
		return nil
	}

	patch := client.MergeFrom(sec.DeepCopy())
	if rollCA {
		newCA, newKey, caErr := newCA()
		if caErr != nil {
			return caErr
		}
		// caBundle() serves both CAs from here until the next call to this
		// branch drops the previous one, so an in-flight client's cached
		// bundle stays valid for one renewal interval (renewBefore) even if
		// it validated against the certificate signed by the old CA. A
		// client that misses that whole window fails TLS verification until
		// it refetches caBundle; there is no separate grace period or
		// revocation mechanism.
		sec.Data[keyCAPrevious] = sec.Data[keyCACert]
		sec.Data[keyCACert] = newCA
		sec.Data[keyCAKey] = newKey
		m.Logger.Info(ctx, "rotated webhook CA; previous CA kept in bundle")
	} else {
		// This renewal has replaced every certificate the previous CA signed;
		// a completed roll drops it from the bundle.
		delete(sec.Data, keyCAPrevious)
	}
	cert, key, err := m.newServingPair(sec.Data[keyCACert], sec.Data[keyCAKey])
	if err != nil {
		return err
	}
	sec.Data[keyTLSCert] = cert
	sec.Data[keyTLSKey] = key
	if err := m.Client.Patch(ctx, sec, patch); err != nil {
		return fmt.Errorf("patch webhook secret: %w", err)
	}
	m.Logger.Info(ctx, "renewed webhook serving certificate")
	return nil
}

// ensureSecret creates the Secret with a fresh CA and serving pair, or returns
// the existing one when another replica won the race.
func (m *Manager) ensureSecret(ctx context.Context) (*corev1.Secret, error) {
	sec := &corev1.Secret{}
	err := m.Client.Get(ctx, client.ObjectKey{Name: SecretName, Namespace: m.Namespace}, sec)
	if err == nil {
		return sec, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get webhook secret: %w", err)
	}
	ca, caKey, err := newCA()
	if err != nil {
		return nil, err
	}
	cert, key, err := m.newServingPair(ca, caKey)
	if err != nil {
		return nil, err
	}
	sec = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName,
			Namespace: m.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "orbit-operator"},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{keyCACert: ca, keyCAKey: caKey, keyTLSCert: cert, keyTLSKey: key},
	}
	if err := m.Client.Create(ctx, sec); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing := &corev1.Secret{}
			if getErr := m.Client.Get(ctx, client.ObjectKey{Name: SecretName, Namespace: m.Namespace}, existing); getErr != nil {
				return nil, fmt.Errorf("get webhook secret after conflict: %w", getErr)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("create webhook secret: %w", err)
	}
	m.Logger.Info(ctx, "created webhook TLS secret")
	return sec, nil
}

// writeCertDir writes the serving pair where the webhook server reads it. The
// server's certificate watcher picks up changes without a restart.
func (m *Manager) writeCertDir(sec *corev1.Secret) error {
	if err := os.MkdirAll(m.CertDir, 0o700); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}
	for key, name := range map[string]string{keyTLSCert: "tls.crt", keyTLSKey: "tls.key"} {
		path := filepath.Join(m.CertDir, name)
		current, _ := os.ReadFile(path)
		if bytes.Equal(current, sec.Data[key]) {
			continue
		}
		if err := os.WriteFile(path, sec.Data[key], 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// patchWebhook sets caBundle (unless cert-manager owns it) and failurePolicy
// Fail on one configuration, and is a no-op when both already match.
func (m *Manager) patchWebhook(ctx context.Context, name string, bundle []byte) error {
	var obj client.Object
	var hooks []*hookFields
	switch name {
	case Webhooks.Mutating:
		mw := &admissionv1.MutatingWebhookConfiguration{}
		if err := m.Client.Get(ctx, client.ObjectKey{Name: name}, mw); err != nil {
			return fmt.Errorf("get %s: %w", name, err)
		}
		for i := range mw.Webhooks {
			hooks = append(hooks, &hookFields{CABundle: &mw.Webhooks[i].ClientConfig.CABundle, FailurePolicy: &mw.Webhooks[i].FailurePolicy})
		}
		obj = mw
	default:
		vw := &admissionv1.ValidatingWebhookConfiguration{}
		if err := m.Client.Get(ctx, client.ObjectKey{Name: name}, vw); err != nil {
			return fmt.Errorf("get %s: %w", name, err)
		}
		for i := range vw.Webhooks {
			hooks = append(hooks, &hookFields{CABundle: &vw.Webhooks[i].ClientConfig.CABundle, FailurePolicy: &vw.Webhooks[i].FailurePolicy})
		}
		obj = vw
	}
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	if !m.wire(hooks, bundle) {
		return nil
	}
	if err := m.Client.Patch(ctx, obj, patch); err != nil {
		return fmt.Errorf("patch %s: %w", name, err)
	}
	m.Logger.Info(ctx, "webhook configuration wired", logging.Field{Key: "name", Value: name})
	return nil
}

// hookFields are the two fields the manager owns on each webhook entry.
type hookFields struct {
	CABundle      *[]byte
	FailurePolicy **admissionv1.FailurePolicyType
}

// wire applies the bundle and Fail policy to every entry; true when anything changed.
func (m *Manager) wire(hooks []*hookFields, bundle []byte) bool {
	fail := admissionv1.Fail
	changed := false
	for _, h := range hooks {
		if !m.External && !bytes.Equal(*h.CABundle, bundle) {
			*h.CABundle = bundle
			changed = true
		}
		if *h.FailurePolicy == nil || **h.FailurePolicy != fail {
			*h.FailurePolicy = &fail
			changed = true
		}
	}
	return changed
}

// caBundle is the current CA plus the previous one during a CA roll.
func caBundle(sec *corev1.Secret) []byte {
	b := append([]byte{}, sec.Data[keyCACert]...)
	if prev := sec.Data[keyCAPrevious]; len(prev) > 0 {
		b = append(b, prev...)
	}
	return b
}

func newCA() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, fmt.Errorf("serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "orbit-operator-webhook-ca"},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create CA: %w", err)
	}
	return encodePair(der, key)
}

func (m *Manager) newServingPair(caPEM, caKeyPEM []byte) (certPEM, keyPEM []byte, err error) {
	caCert, err := parseCert(caPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA: %w", err)
	}
	caKey, err := parseKey(caKeyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate serving key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, fmt.Errorf("serial: %w", err)
	}
	svc := fmt.Sprintf("%s.%s.svc", m.ServiceName, m.Namespace)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: svc},
		DNSNames:     []string{m.ServiceName, svc, svc + ".cluster.local"},
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(servingValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create serving cert: %w", err)
	}
	return encodePair(der, key)
}

func encodePair(der []byte, key *ecdsa.PrivateKey) ([]byte, []byte, error) {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

func parseCert(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}
