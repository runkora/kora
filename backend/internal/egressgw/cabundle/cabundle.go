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
	"strings"
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

// LoadEager scans root for tenant CA material and loads each tenant into
// the bundle. Returns an error if the root is unreadable or any tenant's
// material is malformed. Empty / missing roots are permitted (the operator
// may not have mounted the secret yet) and yield an empty bundle that
// rejects every CONNECT with ErrTenantNotFound until the gateway restarts
// with the secret populated.
//
// On-disk layout:
//
//	{root}/{tenant}.ca.crt
//	{root}/{tenant}.ca.key
//
// This is a flat layout because Kubernetes Secret data keys cannot contain
// "/", and the operator's ClusterCAAggregator writes the aggregated
// orbit-tenant-cas Secret with keys like "acme.ca.crt" / "acme.ca.key".
// When projected as a volume, those become files at the root of the mount.
// Dev and test setups use the same layout so what we test is what
// production runs.
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

	for _, tenant := range tenantsWithCertAndKey(entries) {
		ca, err := loadTenantCA(root, tenant)
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

// loadTenantCA reads {root}/{tenant}.ca.crt + {tenant}.ca.key and
// constructs an IssuingCA.
func loadTenantCA(root, tenant string) (*tlsca.IssuingCA, error) {
	certPEM, err := os.ReadFile(filepath.Join(root, tenant+".ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read %s.ca.crt: %w", tenant, err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(root, tenant+".ca.key"))
	if err != nil {
		return nil, fmt.Errorf("read %s.ca.key: %w", tenant, err)
	}
	return tlsca.New(tenant, certPEM, keyPEM)
}

// tenantsWithCertAndKey returns the set of tenant names for which both
// {tenant}.ca.crt and {tenant}.ca.key are present in the root. A tenant
// with only one half is intentionally skipped (no warning at this layer —
// the operator's aggregator should never produce that state, so silence
// here keeps the log clean during ordinary projection updates).
func tenantsWithCertAndKey(entries []os.DirEntry) []string {
	const certSuffix = ".ca.crt"
	const keySuffix = ".ca.key"
	have := make(map[string]struct{ cert, key bool })
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasSuffix(name, certSuffix):
			t := strings.TrimSuffix(name, certSuffix)
			h := have[t]
			h.cert = true
			have[t] = h
		case strings.HasSuffix(name, keySuffix):
			t := strings.TrimSuffix(name, keySuffix)
			h := have[t]
			h.key = true
			have[t] = h
		}
	}
	var out []string
	for t, h := range have {
		if h.cert && h.key {
			out = append(out, t)
		}
	}
	return out
}
