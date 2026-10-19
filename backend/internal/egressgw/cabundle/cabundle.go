// Package cabundle is the per-tenant CA loader for the egress gateway. It reads
// tenant CA material from the secret root the binary configures, typically a
// Kubernetes Secret mounted at /var/run/orbit/tenant-ca/, and holds one
// tlsca.IssuingCA per tenant in memory.
//
// The mount is flat, one pair of keys per tenant:
//
//	{root}/{tenant}.ca.crt
//	{root}/{tenant}.ca.key
//
// Watch re-reads the mount on a short interval, so a new tenant or a rotated
// CA takes effect without a gateway restart.
package cabundle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/egressgw"
	"github.com/runkora/kora/backend/internal/egressgw/tlsca"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ErrTenantNotFound is returned by For when the bundle has no CA for the
// requested tenant. The egress gateway treats this as a 500, since a tenant CA must
// always be present for any tenant whose agents the gateway serves; absence is
// an operator misconfiguration, not a request-shaped error.
var ErrTenantNotFound = errors.New("no issuing CA for tenant")

// ReloadInterval is how often Watch re-reads the mount. Kubelet takes up to a
// minute to project a Secret change, so a shorter interval buys little.
const ReloadInterval = 10 * time.Second

// Bundle is a thread-safe map of tenantID → *tlsca.IssuingCA. It satisfies
// egressgw.CABundle.
type Bundle struct {
	root   string
	logger logging.Logger

	mu  sync.RWMutex
	cas map[string]tenantCA
}

// tenantCA keeps the PEM it was built from, so a reload can tell an unchanged
// CA apart and keep its leaf cache.
type tenantCA struct {
	ca      *tlsca.IssuingCA
	certPEM []byte
	keyPEM  []byte
}

// For returns the IssuingCA registered for tenant.
func (b *Bundle) For(tenant string) (egressgw.IssuingCA, error) {
	b.mu.RLock()
	entry, ok := b.cas[tenant]
	b.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTenantNotFound, tenant)
	}
	return entry.ca, nil
}

// LoadEager scans root for tenant CA material and loads each tenant into
// the bundle. Returns an error only if the root is unreadable. Empty or
// missing roots are permitted (the operator may not have mounted the secret
// yet) and yield an empty bundle that Watch fills once the files appear.
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
	b := &Bundle{root: root, logger: logger, cas: make(map[string]tenantCA)}

	if root == "" {
		logger.Warn(ctx, "tenant CA root is empty; gateway will reject all CONNECTs")
		return b, nil
	}

	if err := b.Reload(ctx); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Warn(ctx, "tenant CA root does not exist yet; gateway will reject CONNECTs until it appears",
				logging.Field{Key: "root", Value: root})
			return b, nil
		}
		return nil, err
	}

	b.mu.RLock()
	loaded := len(b.cas)
	b.mu.RUnlock()
	logger.Info(ctx, "tenant CAs loaded",
		logging.Field{Key: "root", Value: root},
		logging.Field{Key: "tenants", Value: loaded},
	)
	return b, nil
}

// Watch calls Reload every ReloadInterval until ctx is done.
func (b *Bundle) Watch(ctx context.Context) {
	if b.root == "" {
		return
	}
	ticker := time.NewTicker(ReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.Reload(ctx); err != nil && !errors.Is(err, os.ErrNotExist) {
				b.logger.Error(ctx, "tenant CA reload failed; keeping the loaded CAs",
					logging.Field{Key: "error", Value: err.Error()})
			}
		}
	}
}

// Reload re-reads the mount and swaps in the result. A tenant whose files no
// longer parse keeps its previous CA, so one bad entry never takes down the rest.
func (b *Bundle) Reload(ctx context.Context) error {
	entries, err := os.ReadDir(b.root)
	if err != nil {
		return fmt.Errorf("read tenant CA root %q: %w", b.root, err)
	}

	b.mu.RLock()
	prev := b.cas
	b.mu.RUnlock()

	next := make(map[string]tenantCA, len(prev))
	for _, tenant := range tenantsWithCertAndKey(entries) {
		entry, err := loadTenantCA(b.root, tenant, prev[tenant])
		if err != nil {
			b.logger.Error(ctx, "tenant CA is unreadable; keeping the previous one if any",
				logging.Field{Key: "tenant", Value: tenant},
				logging.Field{Key: "error", Value: err.Error()})
			if old, ok := prev[tenant]; ok {
				next[tenant] = old
			}
			continue
		}
		if entry.ca != prev[tenant].ca {
			b.logger.Info(ctx, "tenant CA loaded", logging.Field{Key: "tenant", Value: tenant})
		}
		next[tenant] = entry
	}

	b.mu.Lock()
	b.cas = next
	b.mu.Unlock()
	return nil
}

// loadTenantCA reads {root}/{tenant}.ca.crt + {tenant}.ca.key and builds an
// IssuingCA. It returns prev unchanged when the files still match it.
func loadTenantCA(root, tenant string, prev tenantCA) (tenantCA, error) {
	certPEM, err := os.ReadFile(filepath.Join(root, tenant+".ca.crt"))
	if err != nil {
		return tenantCA{}, fmt.Errorf("read %s.ca.crt: %w", tenant, err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(root, tenant+".ca.key"))
	if err != nil {
		return tenantCA{}, fmt.Errorf("read %s.ca.key: %w", tenant, err)
	}
	if prev.ca != nil && bytes.Equal(certPEM, prev.certPEM) && bytes.Equal(keyPEM, prev.keyPEM) {
		return prev, nil
	}
	ca, err := tlsca.New(tenant, certPEM, keyPEM)
	if err != nil {
		return tenantCA{}, err
	}
	return tenantCA{ca: ca, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// tenantsWithCertAndKey returns the set of tenant names for which both
// {tenant}.ca.crt and {tenant}.ca.key are present in the root. A tenant
// with only one half is intentionally skipped (no warning at this layer;
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
