// Package cabundle is the per-tenant CA loader for the egress gateway. It
// reads tenant CA material from the secret root configured by the binary —
// typically a Kubernetes Secret mounted at /var/run/orbit/tenant-ca/ — and
// holds one tlsca.IssuingCA per tenant in memory.
//
// On-disk layout (eager loader):
//
//	{root}/{tenant}/ca.crt
//	{root}/{tenant}/ca.key
//
// LoadEager scans {root} for tenant subdirectories at startup. Hot-reload on
// secret rotation is not implemented yet; rotating a tenant's CA requires a
// gateway restart. Restart is acceptable for now — the secret rotation cadence
// is days/months, not minutes — and is tracked as a follow-up.
package cabundle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/clear-route/orbit/internal/egressgw"
	"github.com/clear-route/orbit/internal/egressgw/tlsca"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ErrTenantNotFound is returned by For when the bundle has no CA for the
// requested tenant. The egress gateway treats this as a 500 — a tenant CA must
// always be present for any tenant whose agents the gateway serves; absence is
// an operator misconfiguration, not a request-shaped error.
var ErrTenantNotFound = errors.New("no issuing CA for tenant")

// Bundle is a thread-safe map of tenantID → *tlsca.IssuingCA. It satisfies
// egressgw.CABundle.
type Bundle struct {
	mu  sync.RWMutex
	cas map[string]*tlsca.IssuingCA
}

// For returns the IssuingCA registered for tenant.
func (b *Bundle) For(tenant string) (egressgw.IssuingCA, error) {
	b.mu.RLock()
	ca, ok := b.cas[tenant]
	b.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTenantNotFound, tenant)
	}
	return ca, nil
}

// LoadEager walks root for tenant subdirectories and loads each one's CA
// material into the bundle. Returns an error if the root is missing,
// unreadable, or any tenant's material is malformed. Empty roots are
// permitted — the operator may not have mounted the secret yet — and yield an
// empty bundle that rejects every CONNECT with ErrTenantNotFound until the
// gateway is restarted with the secret mounted.
func LoadEager(ctx context.Context, root string, logger logging.Logger) (*Bundle, error) {
	b := &Bundle{cas: make(map[string]*tlsca.IssuingCA)}

	if root == "" {
		logger.Warn(ctx, "tenant CA root is empty — gateway will reject all CONNECTs")
		return b, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn(ctx, "tenant CA root does not exist — gateway will reject all CONNECTs",
				logging.Field{Key: "root", Value: root})
			return b, nil
		}
		return nil, fmt.Errorf("read tenant CA root %q: %w", root, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		tenant := entry.Name()
		ca, err := loadOne(filepath.Join(root, tenant), tenant)
		if err != nil {
			return nil, fmt.Errorf("load CA for tenant %q: %w", tenant, err)
		}
		b.cas[tenant] = ca
	}

	logger.Info(ctx, "tenant CAs loaded",
		logging.Field{Key: "root", Value: root},
		logging.Field{Key: "tenants", Value: len(b.cas)},
	)
	return b, nil
}

// loadOne reads ca.crt and ca.key from dir and constructs an IssuingCA.
func loadOne(dir, tenant string) (*tlsca.IssuingCA, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read ca.crt: %w", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "ca.key"))
	if err != nil {
		return nil, fmt.Errorf("read ca.key: %w", err)
	}
	return tlsca.New(tenant, certPEM, keyPEM)
}
