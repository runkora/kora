// Package tlsca is the per-tenant TLS issuing authority for the egress gateway.
// It holds one CA key + cert per tenant and mints short-lived leaf certificates
// on demand for SNI hosts the gateway intercepts. The agent never sees the CA
// private key — only the public root, which is mounted into the sandbox image's
// trust store so tools (git, gh, curl, SDKs) accept the leaf the gateway
// presents during MITM termination.
//
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md §TLS
// interception & per-tenant CA.
package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// Sentinel errors. Callers may test for these with errors.Is.
var (
	// ErrInvalidPEM is returned when CA root PEM material cannot be parsed.
	ErrInvalidPEM = errors.New("invalid PEM material")
	// ErrUnsupportedKey is returned when a CA private key is not RSA/ECDSA.
	ErrUnsupportedKey = errors.New("unsupported CA private key type")
)

// Default leaf TTL. Long enough to amortize signing cost across a multi-step
// command (a git push hits info/refs then git-receive-pack on separate
// connections), short enough to bound exposure if the leaf key ever leaked
// (which is hard — leaf keys never leave the broker process).
const defaultLeafTTL = 24 * time.Hour

// leafCacheRefreshMargin is how long before true expiry a cached leaf is
// treated as stale, so a caller never gets an about-to-expire cert.
const leafCacheRefreshMargin = 1 * time.Hour

// IssuingCA mints short-lived leaf certificates signed by a per-tenant CA. One
// IssuingCA serves all leaves for one tenant; the egress gateway holds one per
// tenant it serves. The CA's private key never leaves this process.
//
// Use GetCertificate as the tls.Config callback on the gateway's TLS server to
// issue the right leaf for each ClientHello SNI.
type IssuingCA struct {
	caCert *x509.Certificate
	caKey  any // *rsa.PrivateKey or *ecdsa.PrivateKey
	tenant string
	ttl    time.Duration
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// Option configures an IssuingCA.
type Option func(*IssuingCA)

// WithLeafTTL overrides the default leaf certificate lifetime.
func WithLeafTTL(d time.Duration) Option {
	return func(c *IssuingCA) { c.ttl = d }
}

// New constructs an IssuingCA from PEM-encoded CA cert and private key. Both
// arguments accept any number of PEM blocks; the first CERTIFICATE block is
// used as the CA, and the first PRIVATE KEY block (RSA or EC) as its key.
//
// tenant is the tenant ID this CA serves; it is recorded on every minted leaf
// for audit and is part of the cache key.
func New(tenant string, caCertPEM, caKeyPEM []byte, opts ...Option) (*IssuingCA, error) {
	if tenant == "" {
		return nil, errors.New("tenant id is required")
	}

	cert, err := parseCertPEM(caCertPEM)
	if err != nil {
		return nil, fmt.Errorf("parse ca cert: %w", err)
	}
	if !cert.IsCA {
		return nil, errors.New("provided cert is not a CA")
	}

	key, err := parseKeyPEM(caKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse ca key: %w", err)
	}

	c := &IssuingCA{
		caCert: cert,
		caKey:  key,
		tenant: tenant,
		ttl:    defaultLeafTTL,
		now:    time.Now,
		cache:  make(map[string]*tls.Certificate),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Tenant reports the tenant this CA serves.
func (c *IssuingCA) Tenant() string { return c.tenant }

// RootCertPEM returns the CA root certificate PEM. The egress gateway
// publishes this to the sandbox image trust store so tools accept the leaves
// signed by this CA.
func (c *IssuingCA) RootCertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.caCert.Raw})
}

// GetCertificate is a tls.Config.GetCertificate callback. It mints (or returns
// from cache) a leaf cert for hello.ServerName.
//
// hello.ServerName is the SNI the client sent. For CONNECT-style proxies this
// is the upstream host the agent's tool wants to reach (github.com, etc.).
func (c *IssuingCA) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := strings.ToLower(strings.TrimSpace(hello.ServerName))
	if host == "" {
		return nil, errors.New("client hello missing server_name")
	}
	return c.leafFor(host)
}

// LeafFor mints (or returns cached) a leaf certificate for host. Exposed for
// tests and for explicit pre-warming of well-known hosts at gateway startup.
func (c *IssuingCA) LeafFor(host string) (*tls.Certificate, error) {
	return c.leafFor(strings.ToLower(strings.TrimSpace(host)))
}

func (c *IssuingCA) leafFor(host string) (*tls.Certificate, error) {
	if host == "" {
		return nil, errors.New("host is required")
	}

	c.mu.Lock()
	cached := c.cache[host]
	c.mu.Unlock()
	if cached != nil && c.cacheFresh(cached) {
		return cached, nil
	}

	leaf, err := c.mintLeaf(host)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[host] = leaf
	c.mu.Unlock()
	return leaf, nil
}

func (c *IssuingCA) cacheFresh(leaf *tls.Certificate) bool {
	if leaf.Leaf == nil {
		return false
	}
	return c.now().Before(leaf.Leaf.NotAfter.Add(-leafCacheRefreshMargin))
}

// mintLeaf signs a fresh leaf cert for host. The leaf key is freshly generated
// per host so a leaked leaf key only affects one host.
func (c *IssuingCA) mintLeaf(host string) (*tls.Certificate, error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	now := c.now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Orbit Egress Gateway"},
			// Tenant is recorded on the leaf for audit/log correlation.
			OrganizationalUnit: []string{"tenant=" + c.tenant},
		},
		NotBefore:   now.Add(-1 * time.Minute), // tolerate small clock skew
		NotAfter:    now.Add(c.ttl),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.caCert, &leafKey.PublicKey, c.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign leaf certificate: %w", err)
	}

	leafCert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse minted leaf: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{der, c.caCert.Raw}, // include CA so clients trusting only the root accept the chain
		PrivateKey:  leafKey,
		Leaf:        leafCert,
	}, nil
}

// ── PEM helpers ──────────────────────────────────────────────────────────────

func parseCertPEM(in []byte) (*x509.Certificate, error) {
	for {
		block, rest := pem.Decode(in)
		if block == nil {
			return nil, ErrInvalidPEM
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		in = rest
	}
}

func parseKeyPEM(in []byte) (any, error) {
	for {
		block, rest := pem.Decode(in)
		if block == nil {
			return nil, ErrInvalidPEM
		}
		switch block.Type {
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(block.Bytes)
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse pkcs8: %w", err)
			}
			switch k.(type) {
			case *rsa.PrivateKey, *ecdsa.PrivateKey:
				return k, nil
			default:
				return nil, ErrUnsupportedKey
			}
		}
		in = rest
	}
}

// randomSerial returns a 128-bit positive serial number, as RFC 5280 requires.
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
