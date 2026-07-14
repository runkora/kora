package orbitapi

import (
	"crypto/sha256"
	"encoding/hex"
)

// CSRFingerprint returns a stable, non-secret identifier for a PEM-encoded
// CSR, used to detect when spec.KubeconfigCSR has changed to a new request
// (decision 6). The operator stores the fingerprint of the CSR it last
// signed in status.KubeconfigCSRFingerprint rather than treating a non-empty
// status.KubeconfigCertificate as "already fulfilled forever" — otherwise a
// second kubeconfig request (a fresh keypair + CSR) would be silently
// ignored and the caller would receive a certificate that does not match
// the private key it just generated. Shared here, rather than duplicated in
// internal/operator and internal/devenvironments, so both sides compute the
// identical value.
func CSRFingerprint(csrPEM []byte) string {
	sum := sha256.Sum256(csrPEM)
	return hex.EncodeToString(sum[:])
}
