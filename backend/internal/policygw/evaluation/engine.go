package evaluation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
)

// ErrBundleUnexpectedStatus is returned when the bundle endpoint responds
// with a non-200 status code.
var ErrBundleUnexpectedStatus = errors.New("bundle endpoint returned unexpected status")

const allowQuery = "data.orbit.authz.allow"

// tenantBundle holds a compiled PreparedEvalQuery for a single tenant.
type tenantBundle struct {
	query    rego.PreparedEvalQuery
	loadedAt time.Time
	revision string
}

// Engine manages per-tenant OPA bundle loading and policy evaluation.
// It polls the Policy Service bundle endpoint for each active tenant and
// caches a PreparedEvalQuery per tenant. All evaluation is in-process.
type Engine struct {
	*httpclient.Base
	mu             sync.RWMutex
	bundles        map[string]*tenantBundle // tenant_id → compiled bundle
	bundleEndpoint string                   // base URL: http://policy-service/v1/policy/bundles
	pollInterval   time.Duration
}

// NewEngine creates an evaluation Engine that polls bundles from bundleEndpoint.
// bundleEndpoint should be the full base URL including the path prefix, e.g.:
//
//	http://policy-service.orbit-system.svc/v1/policy/bundles
//
// pollInterval controls how often cached bundles are refreshed.
func NewEngine(bundleEndpoint string, pollInterval time.Duration, logger logging.Logger) *Engine {
	return &Engine{
		Base:           httpclient.New("orbit.policy-engine-bundle-client", logger, 10*time.Second),
		bundles:        make(map[string]*tenantBundle),
		bundleEndpoint: bundleEndpoint,
		pollInterval:   pollInterval,
	}
}

// StartPoller launches a background goroutine that periodically refreshes
// bundles for all known tenants. It returns immediately. Call with go or
// as part of the service startup.
func (e *Engine) StartPoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(e.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.refreshAll(ctx)
			}
		}
	}()
}

// refreshAll refreshes bundles for all currently loaded tenants.
func (e *Engine) refreshAll(ctx context.Context) {
	e.mu.RLock()
	tenants := make([]string, 0, len(e.bundles))
	for t := range e.bundles {
		tenants = append(tenants, t)
	}
	e.mu.RUnlock()

	for _, tenantID := range tenants {
		if err := e.LoadBundle(ctx, tenantID); err != nil {
			e.Logger().Error(ctx, "bundle poll failed",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}

// LoadBundle fetches and compiles the OPA bundle for a tenant from the
// Policy Service. Replaces any existing cached bundle for that tenant.
func (e *Engine) LoadBundle(ctx context.Context, tenantID string) error {
	url := e.bundleEndpoint + "/" + tenantID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create bundle request: %w", err)
	}

	_, span, resp, err := e.Do(ctx, "policy-engine.Engine.LoadBundle", req)
	defer span.End()
	if err != nil {
		return fmt.Errorf("fetch bundle for tenant %s: %w", tenantID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No grants configured for this tenant yet — this is valid, not an error.
		e.Logger().Info(ctx, "no bundle available for tenant (no grants configured)",
			logging.Field{Key: "tenant_id", Value: tenantID},
		)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return e.HandleErrorResponse(span, resp, ErrBundleUnexpectedStatus)
	}

	bundleBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read bundle for tenant %s: %w", tenantID, err)
	}

	revision := resp.Header.Get("ETag")

	if err := e.compileBundle(ctx, tenantID, bundleBytes, revision); err != nil {
		return fmt.Errorf("compile bundle for tenant %s: %w", tenantID, err)
	}

	e.Logger().Info(ctx, "bundle loaded",
		logging.Field{Key: "tenant_id", Value: tenantID},
		logging.Field{Key: "revision", Value: revision},
	)
	return nil
}

// compileBundle parses the .tar.gz bundle bytes and prepares an OPA query.
func (e *Engine) compileBundle(ctx context.Context, tenantID string, bundleBytes []byte, revision string) error {
	b, err := bundle.NewReader(bytes.NewReader(bundleBytes)).Read()
	if err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}

	store := inmem.New()

	r := rego.New(
		rego.Query(allowQuery),
		rego.Store(store),
		rego.ParsedBundle(tenantID, &b),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("prepare rego eval: %w", err)
	}

	e.mu.Lock()
	e.bundles[tenantID] = &tenantBundle{
		query:    pq,
		loadedAt: time.Now().UTC(),
		revision: revision,
	}
	e.mu.Unlock()

	return nil
}

// Result holds the outcome of a policy evaluation.
type Result struct {
	Allow  bool
	Reason string
}

// Evaluate evaluates the allow rule for the given input against the tenant's bundle.
// Returns an error if no bundle is loaded for the tenant.
func (e *Engine) Evaluate(ctx context.Context, tenantID string, input Input) (Result, error) {
	e.mu.RLock()
	tb, ok := e.bundles[tenantID]
	e.mu.RUnlock()

	if !ok {
		// Attempt a lazy load on first request for this tenant.
		if err := e.LoadBundle(ctx, tenantID); err != nil {
			return Result{}, fmt.Errorf("no bundle loaded for tenant %s: %w", tenantID, err)
		}
		e.mu.RLock()
		tb, ok = e.bundles[tenantID]
		e.mu.RUnlock()
		if !ok {
			return Result{Reason: "no policy configured for tenant"}, nil
		}
	}

	rs, err := tb.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return Result{}, fmt.Errorf("rego eval: %w", err)
	}

	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return Result{Reason: "no policy result"}, nil
	}

	allow, _ := rs[0].Expressions[0].Value.(bool)
	reason := "deny"
	if allow {
		reason = "allow"
	}

	return Result{Allow: allow, Reason: reason}, nil
}
