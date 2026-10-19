// Tenant CA provisioning for the credential broker (Phase 4b).
//
// The egress gateway terminates outbound TLS for sandbox-resident tools using
// a per-tenant CA's leaf certs. The CA's private key never leaves the gateway
// process; the CA's public root is mounted into the sandbox image's trust
// store so tools accept the gateway's leaves.
//
// This file is the operator side of that contract: on first reconcile we
// generate a fresh self-signed CA per tenant and persist it as the
// orbit-tenant-ca Secret in the tenant's namespace. Subsequent reconciles are
// idempotent — the secret is only created if absent. Rotation is intentionally
// out of scope here; the rotation cadence is days/months and rotating a CA
// breaks all in-flight cached leaves on the gateway, so it must be a
// deliberate operator action, not a reconcile loop.
package operator

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// tenantCASecretName is the Secret in each tenant namespace holding the
// tenant's CA cert + key. The aggregator copies them into orbit-tenant-cas,
// which the egress gateway mounts and re-reads; the sandbox pod projects
// ca.crt only into its trust store.
const tenantCASecretName = "orbit-tenant-ca"

// caCertValidity is how long a freshly-minted tenant CA is valid for. Ten
// years is the typical operator-managed root CA lifetime; rotation before
// expiry is a deliberate, manual procedure.
const caCertValidity = 10 * 365 * 24 * time.Hour

// ensureTenantCA reconciles the orbit-tenant-ca Secret in the tenant
// namespace. If the secret does not exist, a fresh ECDSA P-256 self-signed
// root CA is generated and persisted; if it exists, it is left alone.
//
// The shape of the secret matches what cabundle.LoadEager expects under
// {tenantCARoot}/{tenant}/, so the gateway's projected volume can consume it
// without translation.
func (r *TenantReconciler) ensureTenantCA(ctx context.Context, tenant *OrbitTenant) error {
	ns := tenantNamespace(tenant)

	existing := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: tenantCASecretName, Namespace: ns}, existing)
	if err == nil {
		// Already provisioned. Don't churn it on every reconcile.
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get %s/%s: %w", ns, tenantCASecretName, err)
	}

	certPEM, keyPEM, err := generateTenantCA(tenant.Name)
	if err != nil {
		return fmt.Errorf("generate tenant CA for %q: %w", tenant.Name, err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantCASecretName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "orbit-operator",
				"orbit.io/tenant":              tenant.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": certPEM,
			"ca.key": keyPEM,
		},
	}
	if err := r.Create(ctx, secret); err != nil {
		// Race: another reconcile-thread created it between our Get and Create.
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create %s/%s: %w", ns, tenantCASecretName, err)
	}

	r.Logger.Info(ctx, "tenant CA generated",
		logging.Field{Key: "tenant", Value: tenant.Name},
		logging.Field{Key: "namespace", Value: ns},
		logging.Field{Key: "validity", Value: caCertValidity.String()},
	)
	return nil
}

// generateTenantCA mints a fresh self-signed ECDSA P-256 root CA suitable for
// per-tenant TLS interception. Returns PEM-encoded cert and PKCS#8 PEM-encoded
// key — the shape tlsca.New expects.
func generateTenantCA(tenant string) (certPEM, keyPEM []byte, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "kora Tenant CA — " + tenant,
			Organization: []string{"kora Egress Gateway"},
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(caCertValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("sign CA cert: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal CA key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// randomSerial returns a 128-bit positive serial, per RFC 5280.
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
